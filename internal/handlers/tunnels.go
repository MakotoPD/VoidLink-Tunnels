package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tunnel-api/internal/config"
	"tunnel-api/internal/database"
	"tunnel-api/internal/middleware"
	"tunnel-api/internal/models"
	"tunnel-api/internal/services"
)

type TunnelHandler struct {
	config           *config.Config
	subdomainService *services.SubdomainService
	tunnelService    *services.TunnelService
}

func NewTunnelHandler(cfg *config.Config, subdomainSvc *services.SubdomainService, tunnelSvc *services.TunnelService) *TunnelHandler {
	return &TunnelHandler{
		config:           cfg,
		subdomainService: subdomainSvc,
		tunnelService:    tunnelSvc,
	}
}

// GET /api/tunnels
func (h *TunnelHandler) List(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	rows, err := database.Pool.Query(ctx,
		`SELECT id, user_id, name, subdomain, region, is_active,
		        mc_local_port, http_local_port, udp_local_port, udp_public_port,
		        created_at, updated_at
		 FROM tunnels WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tunnels"})
		return
	}
	defer rows.Close()

	tunnels := []models.TunnelResponse{}
	for rows.Next() {
		var t models.Tunnel
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.Name, &t.Subdomain, &t.Region, &t.IsActive,
			&t.MCLocalPort, &t.HTTPLocalPort, &t.UDPLocalPort, &t.UDPPublicPort,
			&t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			continue
		}
		tunnels = append(tunnels, t.ToResponse(h.config.Domain))
	}

	c.JSON(http.StatusOK, models.TunnelListResponse{
		Tunnels: tunnels,
		Count:   len(tunnels),
		Limit:   h.config.MaxTunnels,
	})
}

// POST /api/tunnels
func (h *TunnelHandler) Create(c *gin.Context) {
	var req models.CreateTunnelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	// Apply defaults
	if req.MCLocalPort == 0 {
		req.MCLocalPort = 25565
	}
	if req.UDPLocalPort == 0 {
		req.UDPLocalPort = 24454
	}

	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	// Check tunnel limit
	var count int
	if err := database.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM tunnels WHERE user_id = $1`, userID,
	).Scan(&count); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check tunnel limit"})
		return
	}
	if count >= h.config.MaxTunnels {
		c.JSON(http.StatusForbidden, gin.H{
			"error": fmt.Sprintf("Tunnel limit reached (%d/%d)", count, h.config.MaxTunnels),
		})
		return
	}

	// Generate unique subdomain
	var subdomain string
	var err error
	for attempts := 0; attempts < 10; attempts++ {
		subdomain, err = h.subdomainService.Generate()
		if err != nil {
			continue
		}
		var exists bool
		database.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM tunnels WHERE subdomain = $1)`, subdomain,
		).Scan(&exists)
		if !exists {
			break
		}
		subdomain = ""
	}
	if subdomain == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate unique subdomain"})
		return
	}

	// Allocate a stable UDP public port from the pool
	udpPublicPort, err := h.allocateUDPPort(ctx)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "No available UDP ports"})
		return
	}

	// Create tunnel record
	var tunnelID uuid.UUID
	err = database.Pool.QueryRow(ctx,
		`INSERT INTO tunnels (user_id, name, subdomain, region, mc_local_port, http_local_port, udp_local_port, udp_public_port)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id`,
		userID, req.Name, subdomain, h.config.Region,
		req.MCLocalPort, req.HTTPLocalPort, req.UDPLocalPort, udpPublicPort,
	).Scan(&tunnelID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create tunnel"})
		return
	}

	t := models.Tunnel{
		ID:            tunnelID,
		UserID:        userID,
		Name:          req.Name,
		Subdomain:     subdomain,
		Region:        h.config.Region,
		IsActive:      false,
		MCLocalPort:   req.MCLocalPort,
		HTTPLocalPort: req.HTTPLocalPort,
		UDPLocalPort:  req.UDPLocalPort,
		UDPPublicPort: &udpPublicPort,
	}
	c.JSON(http.StatusCreated, t.ToResponse(h.config.Domain))
}

// GET /api/tunnels/:id
func (h *TunnelHandler) Get(c *gin.Context) {
	tunnelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tunnel ID"})
		return
	}

	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	var t models.Tunnel
	err = database.Pool.QueryRow(ctx,
		`SELECT id, user_id, name, subdomain, region, is_active,
		        mc_local_port, http_local_port, udp_local_port, udp_public_port,
		        created_at, updated_at
		 FROM tunnels WHERE id = $1 AND user_id = $2`,
		tunnelID, userID,
	).Scan(
		&t.ID, &t.UserID, &t.Name, &t.Subdomain, &t.Region, &t.IsActive,
		&t.MCLocalPort, &t.HTTPLocalPort, &t.UDPLocalPort, &t.UDPPublicPort,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tunnel not found"})
		return
	}

	c.JSON(http.StatusOK, t.ToResponse(h.config.Domain))
}

// PATCH /api/tunnels/:id
func (h *TunnelHandler) Update(c *gin.Context) {
	tunnelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tunnel ID"})
		return
	}

	var req models.UpdateTunnelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	var t models.Tunnel
	err = database.Pool.QueryRow(ctx,
		`SELECT id, subdomain, is_active, name, mc_local_port, http_local_port, udp_local_port, udp_public_port
		 FROM tunnels WHERE id = $1 AND user_id = $2`,
		tunnelID, userID,
	).Scan(&t.ID, &t.Subdomain, &t.IsActive, &t.Name, &t.MCLocalPort, &t.HTTPLocalPort, &t.UDPLocalPort, &t.UDPPublicPort)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tunnel not found"})
		return
	}

	if t.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Stop the tunnel before editing it"})
		return
	}

	// Apply only provided fields
	if req.Name != nil {
		if len(*req.Name) < 1 || len(*req.Name) > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Name must be 1-100 characters"})
			return
		}
		t.Name = *req.Name
	}
	if req.MCLocalPort != nil {
		t.MCLocalPort = *req.MCLocalPort
	}
	if req.HTTPLocalPort != nil {
		if *req.HTTPLocalPort == 0 {
			t.HTTPLocalPort = nil
		} else {
			t.HTTPLocalPort = req.HTTPLocalPort
		}
	}
	if req.UDPLocalPort != nil {
		t.UDPLocalPort = *req.UDPLocalPort
	}

	_, err = database.Pool.Exec(ctx,
		`UPDATE tunnels SET name=$1, mc_local_port=$2, http_local_port=$3, udp_local_port=$4, updated_at=NOW()
		 WHERE id = $5`,
		t.Name, t.MCLocalPort, t.HTTPLocalPort, t.UDPLocalPort, tunnelID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tunnel"})
		return
	}

	t.Region = h.config.Region
	c.JSON(http.StatusOK, t.ToResponse(h.config.Domain))
}

// DELETE /api/tunnels/:id
func (h *TunnelHandler) Delete(c *gin.Context) {
	tunnelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tunnel ID"})
		return
	}

	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	var t models.Tunnel
	err = database.Pool.QueryRow(ctx,
		`SELECT id, subdomain, is_active, mc_local_port, http_local_port, udp_local_port, udp_public_port
		 FROM tunnels WHERE id = $1 AND user_id = $2`,
		tunnelID, userID,
	).Scan(&t.ID, &t.Subdomain, &t.IsActive, &t.MCLocalPort, &t.HTTPLocalPort, &t.UDPLocalPort, &t.UDPPublicPort)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tunnel not found"})
		return
	}

	if t.IsActive {
		h.tunnelService.StopTunnel(t)
	}

	_, err = database.Pool.Exec(ctx, `DELETE FROM tunnels WHERE id = $1`, tunnelID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete tunnel"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tunnel deleted"})
}

// POST /api/tunnels/:id/start
func (h *TunnelHandler) Start(c *gin.Context) {
	tunnelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tunnel ID"})
		return
	}

	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	var t models.Tunnel
	err = database.Pool.QueryRow(ctx,
		`SELECT id, subdomain, is_active, mc_local_port, http_local_port, udp_local_port, udp_public_port
		 FROM tunnels WHERE id = $1 AND user_id = $2`,
		tunnelID, userID,
	).Scan(&t.ID, &t.Subdomain, &t.IsActive, &t.MCLocalPort, &t.HTTPLocalPort, &t.UDPLocalPort, &t.UDPPublicPort)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tunnel not found"})
		return
	}
	if t.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tunnel is already active"})
		return
	}

	if err := h.tunnelService.StartTunnel(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start tunnel: " + err.Error()})
		return
	}

	_, err = database.Pool.Exec(ctx,
		`UPDATE tunnels SET is_active = TRUE, updated_at = NOW() WHERE id = $1`, tunnelID,
	)
	if err != nil {
		h.tunnelService.StopTunnel(t)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tunnel status"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tunnel started"})
}

// POST /api/tunnels/:id/stop
func (h *TunnelHandler) Stop(c *gin.Context) {
	tunnelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tunnel ID"})
		return
	}

	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	var t models.Tunnel
	err = database.Pool.QueryRow(ctx,
		`SELECT id, subdomain, is_active, mc_local_port, http_local_port, udp_local_port, udp_public_port
		 FROM tunnels WHERE id = $1 AND user_id = $2`,
		tunnelID, userID,
	).Scan(&t.ID, &t.Subdomain, &t.IsActive, &t.MCLocalPort, &t.HTTPLocalPort, &t.UDPLocalPort, &t.UDPPublicPort)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tunnel not found"})
		return
	}
	if !t.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tunnel is not active"})
		return
	}

	h.tunnelService.StopTunnel(t)

	_, err = database.Pool.Exec(ctx,
		`UPDATE tunnels SET is_active = FALSE, updated_at = NOW() WHERE id = $1`, tunnelID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tunnel status"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tunnel stopped"})
}

// ---- Helpers ----

// allocateUDPPort finds a public port from the pool that is not already assigned in the DB.
func (h *TunnelHandler) allocateUDPPort(ctx context.Context) (int, error) {
	for port := h.config.MinPort; port <= h.config.MaxPort; port++ {
		var exists bool
		database.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM tunnels WHERE udp_public_port = $1)`, port,
		).Scan(&exists)
		if !exists {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available UDP ports in range %d-%d", h.config.MinPort, h.config.MaxPort)
}
