package config

import (
	"os"
	"strconv"
)

type Config struct {
	// Server
	ServerPort string
	ServerHost string

	// Database
	DatabaseURL string

	// JWT
	JWTSecret          string
	JWTAccessTokenTTL  int // minutes
	JWTRefreshTokenTTL int // days

	// Built-in tunnel server
	TunnelPort    int // port for client control connections (default 7001)
	MCProxyPort   int // shared Minecraft TCP listener (default 25565)
	HTTPProxyPort int // shared HTTP proxy listener (default 80)

	// Tunnels
	MinPort    int
	MaxPort    int
	MaxTunnels int
	Domain     string
	Region     string

	// SMTP for password reset
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string
}

func Load() *Config {
	return &Config{
		// Server
		ServerPort: getEnv("SERVER_PORT", "8080"),
		ServerHost: getEnv("SERVER_HOST", "0.0.0.0"),

		// Database
		DatabaseURL: getEnv("DATABASE_URL", "postgres://tunnel:tunnel@localhost:5432/tunneldb?sslmode=disable"),

		// JWT
		JWTSecret:          getEnv("JWT_SECRET", "change-this-in-production-very-secret-key-32chars"),
		JWTAccessTokenTTL:  getEnvInt("JWT_ACCESS_TTL", 60),      // 1 hour
		JWTRefreshTokenTTL: getEnvInt("JWT_REFRESH_TTL", 7),      // 7 days

		// Built-in tunnel server
		TunnelPort:    getEnvInt("TUNNEL_PORT", 7001),
		MCProxyPort:   getEnvInt("MC_PROXY_PORT", 25565),
		HTTPProxyPort: getEnvInt("HTTP_PROXY_PORT", 80),

		// Tunnels
		MinPort:    getEnvInt("MIN_PORT", 20000),
		MaxPort:    getEnvInt("MAX_PORT", 30000),
		MaxTunnels: getEnvInt("MAX_TUNNELS", 3),
		Domain:     getEnv("DOMAIN", "eu.yourdomain.com"),
		Region:     getEnv("REGION", "eu"),

		// SMTP
		SMTPHost:     getEnv("SMTP_HOST", ""),
		SMTPPort:     getEnvInt("SMTP_PORT", 587),
		SMTPUser:     getEnv("SMTP_USER", ""),
		SMTPPassword: getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:     getEnv("SMTP_FROM", "noreply@yourdomain.com"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
