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

func (e *EmailService) SendPasswordReset(toEmail, resetToken string) error {
	if !e.IsConfigured() {
		return fmt.Errorf("SMTP not configured")
	}

	subject := "VoidLink - Password Reset Code"
	
	// Aesthetic HTML template with dark theme support
	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background-color: #f4f4f9; margin: 0; padding: 0; }
        .container { max-width: 600px; margin: 40px auto; background-color: #ffffff; border-radius: 12px; box-shadow: 0 4px 20px rgba(0,0,0,0.05); overflow: hidden; }
        .header { background: linear-gradient(135deg, #3b82f6 0%%, #2dd4bf 100%%); padding: 30px; text-align: center; }
        .header h1 { color: white; margin: 0; font-size: 24px; font-weight: 600; letter-spacing: 0.5px; }
        .content { padding: 40px; color: #334155; text-align: center; }
        .message { font-size: 16px; line-height: 1.6; margin-bottom: 30px; }
        .code-box { background-color: #f1f5f9; border: 2px dashed #cbd5e1; border-radius: 8px; padding: 20px; margin: 20px 0; display: inline-block; }
        .code { font-family: 'Consolas', 'Monaco', monospace; font-size: 32px; font-weight: bold; color: #0f172a; letter-spacing: 4px; }
        .note { font-size: 13px; color: #94a3b8; margin-top: 30px; }
        .footer { background-color: #f8fafc; padding: 20px; text-align: center; color: #94a3b8; font-size: 12px; border-top: 1px solid #e2e8f0; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>VoidLink</h1>
        </div>
        <div class="content">
            <h2>Password Reset Request</h2>
            <p class="message">We received a request to reset your password. Use the code below to complete the process. This code will expire in 1 hour.</p>
            
            <div class="code-box">
                <div class="code">%s</div>
            </div>

            <div style="margin: 30px 0;">
                <a href="voidlink://reset-password?code=%s" style="background-color: #3b82f6; border: 1px solid #3b82f6; color: #ffffff; padding: 12px 24px; text-decoration: none; border-radius: 6px; font-weight: bold; display: inline-block;">Reset Password in App</a>
            </div>
            
            <p class="message" style="margin-bottom:0">If you didn't request this, you can safely ignore this email.</p>
        </div>
        <div class="footer">
            &copy; 2026 MakotoPD. All rights reserved.<br>
            This is an automated message, please do not reply.
        </div>
    </div>
</body>
</html>`, resetToken, resetToken)

	return e.sendEmail(toEmail, subject, htmlBody)
}

func (e *EmailService) sendEmail(to, subject, body string) error {
	from := e.config.SMTPFrom
	host := e.config.SMTPHost
	port := e.config.SMTPPort
	user := e.config.SMTPUser
	password := e.config.SMTPPassword

	// Format message with HTML content type
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=\"utf-8\"\r\n\r\n%s",
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
