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

	// FRP
	FRPServerAddr string
	FRPServerPort int
	FRPToken      string

	// Tunnels
	MinPort    int
	MaxPort    int
	MaxTunnels int
	Domain     string
	Region     string
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

		// FRP
		FRPServerAddr: getEnv("FRP_SERVER_ADDR", "0.0.0.0"),
		FRPServerPort: getEnvInt("FRP_SERVER_PORT", 7000),
		FRPToken:      getEnv("FRP_TOKEN", "frp-secret-token"),

		// Tunnels
		MinPort:    getEnvInt("MIN_PORT", 20000),
		MaxPort:    getEnvInt("MAX_PORT", 30000),
		MaxTunnels: getEnvInt("MAX_TUNNELS", 3),
		Domain:     getEnv("DOMAIN", "eu.makoto.com.pl"),
		Region:     getEnv("REGION", "eu"),
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
