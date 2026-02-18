package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Tunnel struct {
	ID            uuid.UUID `json:"id"`
	UserID        uuid.UUID `json:"user_id"`
	Name          string    `json:"name"`
	Subdomain     string    `json:"subdomain"`
	Region        string    `json:"region"`
	IsActive      bool      `json:"is_active"`
	MCLocalPort   int       `json:"mc_local_port"`   // local Minecraft server port
	HTTPLocalPort *int      `json:"http_local_port"` // local HTTP port for web map (nil = disabled)
	UDPLocalPort  int       `json:"udp_local_port"`  // local voice chat UDP port
	UDPPublicPort *int      `json:"udp_public_port"` // allocated public UDP port (stable)
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type TunnelResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Subdomain string    `json:"subdomain"`
	Region    string    `json:"region"`
	IsActive  bool      `json:"is_active"`

	// Minecraft TCP — players connect without specifying port (standard 25565)
	MCAddress   string `json:"mc_address"`
	MCLocalPort int    `json:"mc_local_port"`

	// HTTP (optional) — web map like Dynmap/BlueMap
	HTTPAddress   *string `json:"http_address"`
	HTTPLocalPort *int    `json:"http_local_port"`

	// UDP Voice Chat — one dedicated port per tunnel
	UDPAddress    string `json:"udp_address"`
	UDPPublicPort int    `json:"udp_public_port"`
	UDPLocalPort  int    `json:"udp_local_port"`

	CreatedAt time.Time `json:"created_at"`
}

func (t *Tunnel) ToResponse(domain string) TunnelResponse {
	fullAddr := t.Subdomain + "." + domain
	resp := TunnelResponse{
		ID:           t.ID,
		Name:         t.Name,
		Subdomain:    t.Subdomain,
		Region:       t.Region,
		IsActive:     t.IsActive,
		MCAddress:    fullAddr,
		MCLocalPort:  t.MCLocalPort,
		UDPLocalPort: t.UDPLocalPort,
		CreatedAt:    t.CreatedAt,
	}

	if t.HTTPLocalPort != nil {
		httpAddr := "map." + fullAddr
		resp.HTTPAddress = &httpAddr
		resp.HTTPLocalPort = t.HTTPLocalPort
	}

	if t.UDPPublicPort != nil {
		resp.UDPPublicPort = *t.UDPPublicPort
		resp.UDPAddress = fmt.Sprintf("%s:%d", fullAddr, *t.UDPPublicPort)
	}

	return resp
}

// Request DTOs

type CreateTunnelRequest struct {
	Name          string `json:"name" binding:"required,min=1,max=100"`
	MCLocalPort   int    `json:"mc_local_port"`   // defaults to 25565
	HTTPLocalPort *int   `json:"http_local_port"` // nil = disabled
	UDPLocalPort  int    `json:"udp_local_port"`  // defaults to 24454
}

type TunnelListResponse struct {
	Tunnels []TunnelResponse `json:"tunnels"`
	Count   int              `json:"count"`
	Limit   int              `json:"limit"`
}

type TunnelConfigResponse struct {
	Info string `json:"info"`
}
