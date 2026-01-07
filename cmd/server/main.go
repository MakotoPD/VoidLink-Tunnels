package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"tunnel-api/internal/config"
	"tunnel-api/internal/database"
	"tunnel-api/internal/handlers"
	"tunnel-api/internal/middleware"
	"tunnel-api/internal/services"
	"tunnel-api/internal/utils"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Connect to database
	if err := database.Connect(cfg.DatabaseURL); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// Run migrations
	if err := database.RunMigrations(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Initialize services
	jwtManager := utils.NewJWTManager(cfg.JWTSecret, cfg.JWTAccessTokenTTL, cfg.JWTRefreshTokenTTL)
	totpService := services.NewTOTPService("MineDash Tunnels")
	subdomainService, _ := services.NewSubdomainService("wordlist/words.txt")
	frpService := services.NewFRPService(cfg.FRPServerAddr, cfg.FRPServerPort, cfg.FRPToken, cfg.Domain)
	emailService := services.NewEmailService(cfg)

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(jwtManager, totpService, emailService)
	twoFactorHandler := handlers.NewTwoFactorHandler(totpService)
	tunnelHandler := handlers.NewTunnelHandler(cfg, subdomainService, frpService)
	healthHandler := handlers.NewHealthHandler(frpService)

	// Setup Gin
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	
	r := gin.Default()

	// CORS middleware
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Health endpoints (public)
	r.GET("/health", healthHandler.Health)
	r.GET("/ping", healthHandler.Ping)

	// API routes
	api := r.Group("/api")
	{
		// Auth routes (public)
		auth := api.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/refresh", authHandler.Refresh)
			auth.POST("/logout", authHandler.Logout)
			auth.POST("/forgot-password", authHandler.ForgotPassword)
			auth.POST("/reset-password", authHandler.ResetPassword)
		}

		// Protected routes
		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware(jwtManager))
		{
			// User
			protected.GET("/auth/me", authHandler.Me)

			// 2FA
			protected.POST("/auth/2fa/setup", twoFactorHandler.Setup)
			protected.POST("/auth/2fa/verify", twoFactorHandler.Verify)
			protected.POST("/auth/2fa/disable", twoFactorHandler.Disable)

			// Tunnels
			protected.GET("/tunnels", tunnelHandler.List)
			protected.POST("/tunnels", tunnelHandler.Create)
			protected.GET("/tunnels/:id", tunnelHandler.Get)
			protected.DELETE("/tunnels/:id", tunnelHandler.Delete)
			protected.POST("/tunnels/:id/start", tunnelHandler.Start)
			protected.POST("/tunnels/:id/stop", tunnelHandler.Stop)
			protected.GET("/tunnels/:id/config", tunnelHandler.GetConfig)
		}
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		database.Close()
		os.Exit(0)
	}()

	// Start server
	addr := fmt.Sprintf("%s:%s", cfg.ServerHost, cfg.ServerPort)
	log.Printf("🚀 Tunnel API starting on %s", addr)
	log.Printf("📍 Domain: %s", cfg.Domain)
	log.Printf("🔧 FRP Server: %s:%d", cfg.FRPServerAddr, cfg.FRPServerPort)
	
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
