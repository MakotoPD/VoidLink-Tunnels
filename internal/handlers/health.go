package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"tunnel-api/internal/database"
	"tunnel-api/internal/services"
)

type HealthHandler struct {
	frpService *services.FRPService
}

func NewHealthHandler(frpService *services.FRPService) *HealthHandler {
	return &HealthHandler{
		frpService: frpService,
	}
}

// GET /health
func (h *HealthHandler) Health(c *gin.Context) {
	// Check database
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
		"active_tunnels": h.frpService.GetActiveProxyCount(),
	})
}

// GET /ping
func (h *HealthHandler) Ping(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "pong"})
}
