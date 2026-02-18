package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"tunnel-api/internal/database"
	"tunnel-api/internal/services"
)

type HealthHandler struct {
	tunnelService *services.TunnelService
}

func NewHealthHandler(tunnelService *services.TunnelService) *HealthHandler {
	return &HealthHandler{tunnelService: tunnelService}
}

// GET /health
func (h *HealthHandler) Health(c *gin.Context) {
	dbOK := true
	if err := database.Pool.Ping(c.Request.Context()); err != nil {
		dbOK = false
	}

	status := "healthy"
	httpStatus := http.StatusOK
	if !dbOK {
		status = "unhealthy"
		httpStatus = http.StatusServiceUnavailable
	}

	c.JSON(httpStatus, gin.H{
		"status":   status,
		"database": dbOK,
	})
}

// GET /ping
func (h *HealthHandler) Ping(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "pong"})
}
