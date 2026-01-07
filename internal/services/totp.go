package services

import (
	"crypto/rand"
	"encoding/base32"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

type TOTPService struct {
	issuer string
}

func NewTOTPService(issuer string) *TOTPService {
	return &TOTPService{
		issuer: issuer,
	}
}

// GenerateSecret creates a new random TOTP secret
func (s *TOTPService) GenerateSecret() (string, error) {
	bytes := make([]byte, 20)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes), nil
}

// GenerateKey creates an OTP key for QR code generation
func (s *TOTPService) GenerateKey(email, secret string) (*otp.Key, error) {
	return totp.Generate(totp.GenerateOpts{
		Issuer:      s.issuer,
		AccountName: email,
		Secret:      []byte(secret),
		SecretSize:  20,
	})
}

// Validate checks if the provided code is valid for the secret
func (s *TOTPService) Validate(secret, code string) bool {
	return totp.Validate(code, secret)
}
