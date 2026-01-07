package models

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID  `json:"id"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"` // Never expose in JSON
	TOTPSecret   *string    `json:"-"` // Never expose
	TOTPEnabled  bool       `json:"totp_enabled"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type UserResponse struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	TOTPEnabled bool      `json:"totp_enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

func (u *User) ToResponse() UserResponse {
	return UserResponse{
		ID:          u.ID,
		Email:       u.Email,
		TOTPEnabled: u.TOTPEnabled,
		CreatedAt:   u.CreatedAt,
	}
}

// Request DTOs
type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	TOTPCode string `json:"totp_code"` // Optional, required if 2FA enabled
}

type AuthResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresIn    int          `json:"expires_in"` // seconds
	User         UserResponse `json:"user"`
	FRPToken     string       `json:"frp_token"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type TOTPSetupResponse struct {
	Secret string `json:"secret"`
	QRCode string `json:"qr_code"` // Base64 encoded PNG
	URL    string `json:"url"`    // otpauth:// URL
}

type TOTPVerifyRequest struct {
	Code string `json:"code" binding:"required,len=6"`
}

type TOTPDisableRequest struct {
	Code     string `json:"code" binding:"required,len=6"`
	Password string `json:"password" binding:"required"`
}
