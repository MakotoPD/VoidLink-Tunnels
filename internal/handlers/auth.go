package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"tunnel-api/internal/config"
	"tunnel-api/internal/database"
	"tunnel-api/internal/middleware"
	"tunnel-api/internal/models"
	"tunnel-api/internal/services"
	"tunnel-api/internal/utils"
)

type AuthHandler struct {
	config       *config.Config
	jwtManager   *utils.JWTManager
	totpService  *services.TOTPService
	emailService *services.EmailService
}

func NewAuthHandler(cfg *config.Config, jwtManager *utils.JWTManager, totpService *services.TOTPService, emailService *services.EmailService) *AuthHandler {
	return &AuthHandler{
		config:       cfg,
		jwtManager:   jwtManager,
		totpService:  totpService,
		emailService: emailService,
	}
}

// POST /api/auth/register
func (h *AuthHandler) Register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	// Normalize email
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process password"})
		return
	}

	// Insert user
	ctx := context.Background()
	var userID uuid.UUID
	err = database.Pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id`,
		req.Email, string(hashedPassword),
	).Scan(&userID)

	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique") {
			c.JSON(http.StatusConflict, gin.H{"error": "Email already registered"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Account created successfully",
		"user_id": userID,
	})
}

// POST /api/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	// Get user from DB
	ctx := context.Background()
	var user models.User
	err := database.Pool.QueryRow(ctx,
		`SELECT id, email, password_hash, totp_secret, totp_enabled, created_at, updated_at 
		 FROM users WHERE email = $1`,
		req.Email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.TOTPSecret, &user.TOTPEnabled, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	// Check 2FA if enabled
	if user.TOTPEnabled {
		if req.TOTPCode == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":         "2FA code required",
				"requires_2fa":  true,
			})
			return
		}
		if user.TOTPSecret == nil || !h.totpService.Validate(*user.TOTPSecret, req.TOTPCode) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid 2FA code"})
			return
		}
	}

	// Generate tokens
	accessToken, err := h.jwtManager.GenerateAccessToken(user.ID, user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	refreshToken, refreshHash, expiresAt, err := h.jwtManager.GenerateRefreshToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate refresh token"})
		return
	}

	// Save refresh token
	_, err = database.Pool.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		user.ID, refreshHash, expiresAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}

	c.JSON(http.StatusOK, models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    h.jwtManager.GetAccessTTLSeconds(),
		User:         user.ToResponse(),
		FRPToken:     h.config.FRPToken,
	})
}

// POST /api/auth/refresh
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req models.RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	tokenHash := h.jwtManager.HashToken(req.RefreshToken)
	ctx := context.Background()

	// Find and validate refresh token
	var userID uuid.UUID
	var expiresAt time.Time
	var tokenID uuid.UUID
	err := database.Pool.QueryRow(ctx,
		`SELECT id, user_id, expires_at FROM refresh_tokens WHERE token_hash = $1`,
		tokenHash,
	).Scan(&tokenID, &userID, &expiresAt)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid refresh token"})
		return
	}

	if time.Now().After(expiresAt) {
		// Delete expired token
		database.Pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE id = $1`, tokenID)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Refresh token expired"})
		return
	}

	// Get user
	var user models.User
	err = database.Pool.QueryRow(ctx,
		`SELECT id, email, totp_enabled, created_at FROM users WHERE id = $1`,
		userID,
	).Scan(&user.ID, &user.Email, &user.TOTPEnabled, &user.CreatedAt)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	// Generate new tokens
	accessToken, err := h.jwtManager.GenerateAccessToken(user.ID, user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	newRefreshToken, newRefreshHash, newExpiresAt, err := h.jwtManager.GenerateRefreshToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate refresh token"})
		return
	}

	// Rotate refresh token (delete old, create new)
	database.Pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE id = $1`, tokenID)
	database.Pool.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, newRefreshHash, newExpiresAt,
	)

	c.JSON(http.StatusOK, models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		ExpiresIn:    h.jwtManager.GetAccessTTLSeconds(),
		User:         user.ToResponse(),
		FRPToken:     h.config.FRPToken,
	})
}

// GET /api/auth/me
func (h *AuthHandler) Me(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	var user models.User
	err := database.Pool.QueryRow(ctx,
		`SELECT id, email, totp_enabled, created_at, updated_at FROM users WHERE id = $1`,
		userID,
	).Scan(&user.ID, &user.Email, &user.TOTPEnabled, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, user.ToResponse())
}

// POST /api/auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	var req models.RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	tokenHash := h.jwtManager.HashToken(req.RefreshToken)
	ctx := context.Background()

	database.Pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE token_hash = $1`, tokenHash)

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

// POST /api/auth/forgot-password
func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email"})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	ctx := context.Background()

	// Check if user exists
	var userID uuid.UUID
	err := database.Pool.QueryRow(ctx,
		`SELECT id FROM users WHERE email = $1`,
		req.Email,
	).Scan(&userID)

	// Always return success to prevent email enumeration
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "If the email exists, a reset code has been sent"})
		return
	}

	// Generate reset token
	resetToken, tokenHash, expiresAt, err := h.jwtManager.GenerateRefreshToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate reset token"})
		return
	}

	// Delete any existing reset tokens for this user
	database.Pool.Exec(ctx, `DELETE FROM password_reset_tokens WHERE user_id = $1`, userID)

	// Save reset token (expires in 1 hour)
	expiresAt = time.Now().Add(1 * time.Hour)
	_, err = database.Pool.Exec(ctx,
		`INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, tokenHash, expiresAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save reset token"})
		return
	}

	// Send email if email service is configured
	// Send email if email service is configured
	if h.emailService != nil && h.emailService.IsConfigured() {
		go h.emailService.SendPasswordReset(req.Email, resetToken)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "If the email exists, a reset code has been sent",
	})
}

// POST /api/auth/reset-password
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req struct {
		Token       string `json:"token" binding:"required"`
		NewPassword string `json:"new_password" binding:"required,min=8"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	tokenHash := h.jwtManager.HashToken(req.Token)
	ctx := context.Background()

	// Find and validate reset token
	var tokenID uuid.UUID
	var userID uuid.UUID
	var expiresAt time.Time
	var used bool
	err := database.Pool.QueryRow(ctx,
		`SELECT id, user_id, expires_at, used FROM password_reset_tokens WHERE token_hash = $1`,
		tokenHash,
	).Scan(&tokenID, &userID, &expiresAt, &used)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired reset token"})
		return
	}

	if used {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Reset token already used"})
		return
	}

	if time.Now().After(expiresAt) {
		database.Pool.Exec(ctx, `DELETE FROM password_reset_tokens WHERE id = $1`, tokenID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Reset token expired"})
		return
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process password"})
		return
	}

	// Update password
	_, err = database.Pool.Exec(ctx,
		`UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`,
		string(hashedPassword), userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}

	// Mark token as used
	database.Pool.Exec(ctx, `UPDATE password_reset_tokens SET used = TRUE WHERE id = $1`, tokenID)

	// Invalidate all refresh tokens for this user (force re-login)
	database.Pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, userID)

	c.JSON(http.StatusOK, gin.H{"message": "Password reset successfully"})
}
