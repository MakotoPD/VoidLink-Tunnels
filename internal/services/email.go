package services

import (
	"crypto/tls"
	"fmt"
	"net/smtp"

	"tunnel-api/internal/config"
)

type EmailService struct {
	config *config.Config
}

func NewEmailService(cfg *config.Config) *EmailService {
	return &EmailService{config: cfg}
}

func (e *EmailService) IsConfigured() bool {
	return e.config.SMTPHost != "" && e.config.SMTPUser != ""
}

func (e *EmailService) SendPasswordReset(toEmail, resetToken, resetURL string) error {
	if !e.IsConfigured() {
		return fmt.Errorf("SMTP not configured")
	}

	subject := "MineDash - Password Reset"
	body := fmt.Sprintf(`Hello,

You have requested to reset your password for MineDash Tunnels.

Click the link below to reset your password (valid for 1 hour):
%s?token=%s

If you didn't request this, please ignore this email.

Best regards,
MineDash Team`, resetURL, resetToken)

	return e.sendEmail(toEmail, subject, body)
}

func (e *EmailService) sendEmail(to, subject, body string) error {
	from := e.config.SMTPFrom
	host := e.config.SMTPHost
	port := e.config.SMTPPort
	user := e.config.SMTPUser
	password := e.config.SMTPPassword

	// Format message
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=\"utf-8\"\r\n\r\n%s",
		from, to, subject, body)

	// Connect to SMTP server
	addr := fmt.Sprintf("%s:%d", host, port)
	auth := smtp.PlainAuth("", user, password, host)

	// Use TLS if port is 465, otherwise STARTTLS
	if port == 465 {
		// Implicit TLS
		tlsConfig := &tls.Config{
			ServerName: host,
		}

		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		defer client.Close()

		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("auth failed: %w", err)
		}

		if err := client.Mail(from); err != nil {
			return fmt.Errorf("mail from failed: %w", err)
		}

		if err := client.Rcpt(to); err != nil {
			return fmt.Errorf("rcpt to failed: %w", err)
		}

		w, err := client.Data()
		if err != nil {
			return fmt.Errorf("data failed: %w", err)
		}

		_, err = w.Write([]byte(message))
		if err != nil {
			return fmt.Errorf("write failed: %w", err)
		}

		err = w.Close()
		if err != nil {
			return fmt.Errorf("close failed: %w", err)
		}

		return client.Quit()
	}

	// Standard STARTTLS (port 587)
	return smtp.SendMail(addr, auth, from, []string{to}, []byte(message))
}
