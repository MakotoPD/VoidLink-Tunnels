package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

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
	frpService       *services.FRPService
}

func NewTunnelHandler(cfg *config.Config, subdomainSvc *services.SubdomainService, frpSvc *services.FRPService) *TunnelHandler {
	return &TunnelHandler{
		config:           cfg,
		subdomainService: subdomainSvc,
		frpService:       frpSvc,
	}
}

// GET /api/tunnels
func (h *TunnelHandler) List(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	rows, err := database.Pool.Query(ctx,
		`SELECT id, user_id, name, subdomain, region, is_active, created_at, updated_at 
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
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Subdomain, &t.Region, &t.IsActive, &t.CreatedAt, &t.UpdatedAt); err != nil {
			continue
		}

		// Fetch ports for this tunnel
		t.Ports, _ = h.fetchTunnelPorts(ctx, t.ID)
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

	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	// Check tunnel limit
	var count int
	err := database.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM tunnels WHERE user_id = $1`,
		userID,
	).Scan(&count)

	if err != nil {
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
	for attempts := 0; attempts < 10; attempts++ {
		subdomain, err = h.subdomainService.Generate()
		if err != nil {
			continue
		}

		// Check if subdomain is unique
		var exists bool
		database.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM tunnels WHERE subdomain = $1)`,
			subdomain,
		).Scan(&exists)

		if !exists {
			break
		}
	}

	if subdomain == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate unique subdomain"})
		return
	}

	// Allocate public ports
	ports := make([]models.TunnelPort, len(req.Ports))
	allocated := make(map[int]bool) // Track allocated ports in this request

	for i, p := range req.Ports {
		publicPort, err := h.allocatePort(ctx, allocated)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "No available ports"})
			return
		}

		// Mark as allocated for subsequent iterations
		allocated[publicPort] = true

		ports[i] = models.TunnelPort{
			Label:      p.Label,
			LocalPort:  p.LocalPort,
			PublicPort: publicPort,
			Protocol:   p.Protocol,
		}
	}

	// Create tunnel
	var tunnelID uuid.UUID
	err = database.Pool.QueryRow(ctx,
		`INSERT INTO tunnels (user_id, name, subdomain, region) 
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, req.Name, subdomain, h.config.Region,
	).Scan(&tunnelID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create tunnel"})
		return
	}

	// Create ports
	for i := range ports {
		ports[i].TunnelID = tunnelID
		var portID uuid.UUID
		err = database.Pool.QueryRow(ctx,
			`INSERT INTO tunnel_ports (tunnel_id, label, local_port, public_port, protocol) 
			 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			tunnelID, ports[i].Label, ports[i].LocalPort, ports[i].PublicPort, ports[i].Protocol,
		).Scan(&portID)

		if err != nil {
			// Rollback: delete tunnel (CASCADE will delete ports)
			database.Pool.Exec(ctx, `DELETE FROM tunnels WHERE id = $1`, tunnelID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to allocate ports"})
			return
		}
		ports[i].ID = portID
	}

	tunnel := models.Tunnel{
		ID:        tunnelID,
		UserID:    userID,
		Name:      req.Name,
		Subdomain: subdomain,
		Region:    h.config.Region,
		IsActive:  false,
		Ports:     ports,
	}

	c.JSON(http.StatusCreated, tunnel.ToResponse(h.config.Domain))
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
		`SELECT id, user_id, name, subdomain, region, is_active, created_at, updated_at 
		 FROM tunnels WHERE id = $1 AND user_id = $2`,
		tunnelID, userID,
	).Scan(&t.ID, &t.UserID, &t.Name, &t.Subdomain, &t.Region, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tunnel not found"})
		return
	}

	t.Ports, _ = h.fetchTunnelPorts(ctx, t.ID)
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

	// Check ownership and get run ID if active
	var frpRunID *string
	var isActive bool
	err = database.Pool.QueryRow(ctx,
		`SELECT frp_run_id, is_active FROM tunnels WHERE id = $1 AND user_id = $2`,
		tunnelID, userID,
	).Scan(&frpRunID, &isActive)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tunnel not found"})
		return
	}

	// Stop tunnel if active
	if isActive && frpRunID != nil {
		h.frpService.StopProxy(*frpRunID)
	}

	// Delete (CASCADE will handle ports)
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
		`SELECT id, user_id, name, subdomain, region, is_active FROM tunnels WHERE id = $1 AND user_id = $2`,
		tunnelID, userID,
	).Scan(&t.ID, &t.UserID, &t.Name, &t.Subdomain, &t.Region, &t.IsActive)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tunnel not found"})
		return
	}

	if t.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tunnel is already active"})
		return
	}

	t.Ports, _ = h.fetchTunnelPorts(ctx, t.ID)

	// Register proxies with FRP server
	runID, err := h.frpService.RegisterProxies(t.Subdomain, t.Ports)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start tunnel: " + err.Error()})
		return
	}

	// Update tunnel status
	_, err = database.Pool.Exec(ctx,
		`UPDATE tunnels SET is_active = TRUE, frp_run_id = $1, updated_at = NOW() WHERE id = $2`,
		runID, tunnelID,
	)

	if err != nil {
		h.frpService.StopProxy(runID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tunnel status"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tunnel started", "run_id": runID})
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

	var frpRunID *string
	var isActive bool
	err = database.Pool.QueryRow(ctx,
		`SELECT frp_run_id, is_active FROM tunnels WHERE id = $1 AND user_id = $2`,
		tunnelID, userID,
	).Scan(&frpRunID, &isActive)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tunnel not found"})
		return
	}

	if !isActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tunnel is not active"})
		return
	}

	if frpRunID != nil {
		h.frpService.StopProxy(*frpRunID)
	}

	_, err = database.Pool.Exec(ctx,
		`UPDATE tunnels SET is_active = FALSE, frp_run_id = NULL, updated_at = NOW() WHERE id = $1`,
		tunnelID,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tunnel status"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tunnel stopped"})
}

// GET /api/tunnels/:id/config
func (h *TunnelHandler) GetConfig(c *gin.Context) {
	tunnelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tunnel ID"})
		return
	}

	userID, _ := middleware.GetUserID(c)
	ctx := context.Background()

	var t models.Tunnel
	err = database.Pool.QueryRow(ctx,
		`SELECT id, subdomain FROM tunnels WHERE id = $1 AND user_id = $2`,
		tunnelID, userID,
	).Scan(&t.ID, &t.Subdomain)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tunnel not found"})
		return
	}

	t.Ports, _ = h.fetchTunnelPorts(ctx, t.ID)

	config := h.frpService.GenerateClientConfig(t.Subdomain, t.Ports)
	c.JSON(http.StatusOK, models.TunnelConfigResponse{
		FRPConfig: config,
	})
}

// Helper functions
func (h *TunnelHandler) fetchTunnelPorts(ctx context.Context, tunnelID uuid.UUID) ([]models.TunnelPort, error) {
	rows, err := database.Pool.Query(ctx,
		`SELECT id, tunnel_id, label, local_port, public_port, protocol 
		 FROM tunnel_ports WHERE tunnel_id = $1`,
		tunnelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ports []models.TunnelPort
	for rows.Next() {
		var p models.TunnelPort
		if err := rows.Scan(&p.ID, &p.TunnelID, &p.Label, &p.LocalPort, &p.PublicPort, &p.Protocol); err != nil {
			continue
		}
		ports = append(ports, p)
	}
	return ports, nil
}

func (h *TunnelHandler) allocatePort(ctx context.Context, excludedPorts map[int]bool) (int, error) {
	for port := h.config.MinPort; port <= h.config.MaxPort; port++ {
		// specific check for excluded ports in this request
		if excludedPorts[port] {
			continue
		}

		var exists bool
		database.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM tunnel_ports WHERE public_port = $1)`,
			port,
		).Scan(&exists)

		if !exists {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports")
}

// Helper for int to string
func itoa(i int) string {
	return strconv.Itoa(i)
}
