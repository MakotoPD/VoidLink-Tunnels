package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"net/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"tunnel-api/internal/database"
	"tunnel-api/internal/middleware"
	"tunnel-api/internal/models"
	"tunnel-api/internal/services"
)

type TwoFactorHandler struct {
	totpService *services.TOTPService
}

func NewTwoFactorHandler(totpService *services.TOTPService) *TwoFactorHandler {
	return &TwoFactorHandler{
		totpService: totpService,
	}
}

// POST /api/auth/2fa/setup
func (h *TwoFactorHandler) Setup(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	email, _ := middleware.GetUserEmail(c)
	ctx := context.Background()

	// Check if 2FA already enabled
	var totpEnabled bool
	err := database.Pool.QueryRow(ctx,
		`SELECT totp_enabled FROM users WHERE id = $1`,
		userID,
	).Scan(&totpEnabled)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	if totpEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "2FA is already enabled"})
		return
	}

	// Generate new TOTP secret
	secret, err := h.totpService.GenerateSecret()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate 2FA secret"})
		return
	}

	// Generate QR code
	key, err := h.totpService.GenerateKey(email, secret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate 2FA key"})
		return
	}

	img, err := key.Image(200, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate QR code"})
		return
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encode QR code"})
		return
	}

	qrBase64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	// Store secret temporarily (not enabled yet)
	_, err = database.Pool.Exec(ctx,
		`UPDATE users SET totp_secret = $1, updated_at = NOW() WHERE id = $2`,
		secret, userID,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save 2FA secret"})
		return
	}

	c.JSON(http.StatusOK, models.TOTPSetupResponse{
		Secret: secret,
		QRCode: "data:image/png;base64," + qrBase64,
		URL:    key.URL(),
	})
}

// POST /api/auth/2fa/verify
func (h *TwoFactorHandler) Verify(c *gin.Context) {
	var req models.TOTPVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	// Get stored secret
	var secret *string
	var totpEnabled bool
	err := database.Pool.QueryRow(ctx,
		`SELECT totp_secret, totp_enabled FROM users WHERE id = $1`,
		userID,
	).Scan(&secret, &totpEnabled)

	if err != nil || secret == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "2FA setup not started"})
		return
	}

	if totpEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "2FA is already enabled"})
		return
	}

	// Validate code
	if !h.totpService.Validate(*secret, req.Code) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid verification code"})
		return
	}

	// Enable 2FA
	_, err = database.Pool.Exec(ctx,
		`UPDATE users SET totp_enabled = TRUE, updated_at = NOW() WHERE id = $1`,
		userID,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable 2FA"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "2FA enabled successfully"})
}

// POST /api/auth/2fa/disable
func (h *TwoFactorHandler) Disable(c *gin.Context) {
	var req models.TOTPDisableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	// Get user data
	var passwordHash string
	var secret *string
	var totpEnabled bool
	err := database.Pool.QueryRow(ctx,
		`SELECT password_hash, totp_secret, totp_enabled FROM users WHERE id = $1`,
		userID,
	).Scan(&passwordHash, &secret, &totpEnabled)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	if !totpEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "2FA is not enabled"})
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}

	// Verify TOTP code
	if secret == nil || !h.totpService.Validate(*secret, req.Code) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 2FA code"})
		return
	}

	// Disable 2FA
	_, err = database.Pool.Exec(ctx,
		`UPDATE users SET totp_enabled = FALSE, totp_secret = NULL, updated_at = NOW() WHERE id = $1`,
		userID,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable 2FA"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "2FA disabled successfully"})
}
