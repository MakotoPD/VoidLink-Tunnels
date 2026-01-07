package models

import (
	"time"

	"github.com/google/uuid"
)

type Tunnel struct {
	ID        uuid.UUID    `json:"id"`
	UserID    uuid.UUID    `json:"user_id"`
	Name      string       `json:"name"`
	Subdomain string       `json:"subdomain"`
	Region    string       `json:"region"`
	IsActive  bool         `json:"is_active"`
	FRPRunID  *string      `json:"-"` // Internal FRP process tracking
	Ports     []TunnelPort `json:"ports"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type TunnelPort struct {
	ID         uuid.UUID `json:"id"`
	TunnelID   uuid.UUID `json:"tunnel_id"`
	Label      string    `json:"label"`
	LocalPort  int       `json:"local_port"`
	PublicPort int       `json:"public_port"`
	Protocol   string    `json:"protocol"` // "tcp" or "udp"
}

type TunnelResponse struct {
	ID          uuid.UUID          `json:"id"`
	Name        string             `json:"name"`
	Subdomain   string             `json:"subdomain"`
	FullAddress string             `json:"full_address"`
	Region      string             `json:"region"`
	IsActive    bool               `json:"is_active"`
	Ports       []TunnelPortResponse `json:"ports"`
	CreatedAt   time.Time          `json:"created_at"`
}

type TunnelPortResponse struct {
	Label      string `json:"label"`
	LocalPort  int    `json:"local_port"`
	PublicPort int    `json:"public_port"`
	Protocol   string `json:"protocol"`
	Address    string `json:"address"` // Full address with port
}

func (t *Tunnel) ToResponse(domain string) TunnelResponse {
	fullAddress := t.Subdomain + "." + domain
	
	ports := make([]TunnelPortResponse, len(t.Ports))
	for i, p := range t.Ports {
		ports[i] = TunnelPortResponse{
			Label:      p.Label,
			LocalPort:  p.LocalPort,
			PublicPort: p.PublicPort,
			Protocol:   p.Protocol,
			Address:    fullAddress + ":" + itoa(p.PublicPort),
		}
	}
	
	return TunnelResponse{
		ID:          t.ID,
		Name:        t.Name,
		Subdomain:   t.Subdomain,
		FullAddress: fullAddress,
		Region:      t.Region,
		IsActive:    t.IsActive,
		Ports:       ports,
		CreatedAt:   t.CreatedAt,
	}
}

func itoa(i int) string {
	return string(rune('0'+i/10000)) + string(rune('0'+(i/1000)%10)) + string(rune('0'+(i/100)%10)) + string(rune('0'+(i/10)%10)) + string(rune('0'+i%10))
}

// Request DTOs
type CreateTunnelRequest struct {
	Name  string            `json:"name" binding:"required,min=1,max=100"`
	Ports []TunnelPortInput `json:"ports" binding:"required,min=1,max=5"`
}

type TunnelPortInput struct {
	Label     string `json:"label" binding:"required,min=1,max=50"`
	LocalPort int    `json:"local_port" binding:"required,min=1,max=65535"`
	Protocol  string `json:"protocol" binding:"required,oneof=tcp udp"`
}

type TunnelConfigResponse struct {
	FRPConfig string `json:"frp_config"` // TOML config for frpc
}

type TunnelListResponse struct {
	Tunnels []TunnelResponse `json:"tunnels"`
	Count   int              `json:"count"`
	Limit   int              `json:"limit"`
}
