package main

import (
	"context"
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
	"tunnel-api/internal/tunnel"
	"tunnel-api/internal/utils"
)

func main() {
	cfg := config.Load()

	// Connect to database
	if err := database.Connect(cfg.DatabaseURL); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	if err := database.RunMigrations(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Create and start the built-in tunnel server (replaces FRP)
	tunnelServer := tunnel.NewServer(
		[]byte(cfg.JWTSecret),
		cfg.TunnelPort,
		cfg.MCProxyPort,
		cfg.HTTPProxyPort,
		cfg.Domain,
		cfg.MinPort,
		cfg.MaxPort,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tunnelServer.Run(ctx); err != nil {
		log.Fatalf("Failed to start tunnel server: %v", err)
	}

	// Initialize services
	jwtManager := utils.NewJWTManager(cfg.JWTSecret, cfg.JWTAccessTokenTTL, cfg.JWTRefreshTokenTTL)
	totpService := services.NewTOTPService("VoidLink Tunnels")
	subdomainService, _ := services.NewSubdomainService("wordlist/words.txt")
	tunnelService := services.NewTunnelService(tunnelServer, cfg.Domain)
	emailService := services.NewEmailService(cfg)

	// Re-register tunnels that were active before server restart
	tunnelService.RestoreActiveTunnels()

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(cfg, jwtManager, totpService, emailService)
	twoFactorHandler := handlers.NewTwoFactorHandler(totpService)
	tunnelHandler := handlers.NewTunnelHandler(cfg, subdomainService, tunnelService)
	healthHandler := handlers.NewHealthHandler(tunnelService)

	// Setup Gin
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	// CORS middleware
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
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
		auth := api.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/refresh", authHandler.Refresh)
			auth.POST("/logout", authHandler.Logout)
			auth.POST("/forgot-password", authHandler.ForgotPassword)
			auth.POST("/reset-password", authHandler.ResetPassword)
		}

		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware(jwtManager))
		{
			protected.GET("/auth/me", authHandler.Me)
			protected.POST("/auth/2fa/setup", twoFactorHandler.Setup)
			protected.POST("/auth/2fa/verify", twoFactorHandler.Verify)
			protected.POST("/auth/2fa/disable", twoFactorHandler.Disable)

			protected.GET("/tunnels", tunnelHandler.List)
			protected.POST("/tunnels", tunnelHandler.Create)
			protected.GET("/tunnels/:id", tunnelHandler.Get)
			protected.PATCH("/tunnels/:id", tunnelHandler.Update)
			protected.DELETE("/tunnels/:id", tunnelHandler.Delete)
			protected.POST("/tunnels/:id/start", tunnelHandler.Start)
			protected.POST("/tunnels/:id/stop", tunnelHandler.Stop)
		}
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		cancel()
		database.Close()
		os.Exit(0)
	}()

	addr := fmt.Sprintf("%s:%s", cfg.ServerHost, cfg.ServerPort)
	log.Printf("VoidLink Tunnel API starting on %s", addr)
	log.Printf("Domain: %s | Tunnel port: %d | MC proxy: %d | HTTP proxy: %d | UDP pool: %d-%d",
		cfg.Domain, cfg.TunnelPort, cfg.MCProxyPort, cfg.HTTPProxyPort, cfg.MinPort, cfg.MaxPort)

	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
