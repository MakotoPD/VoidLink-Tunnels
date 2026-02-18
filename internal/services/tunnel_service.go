package services

import (
	"context"
	"log"

	"tunnel-api/internal/database"
	"tunnel-api/internal/models"
	"tunnel-api/internal/tunnel"
)

// TunnelService is the API-layer service that manages tunnel lifecycle.
// It delegates actual networking to tunnel.Server.
type TunnelService struct {
	server *tunnel.Server
	domain string
}

func NewTunnelService(srv *tunnel.Server, domain string) *TunnelService {
	return &TunnelService{server: srv, domain: domain}
}

// StartTunnel registers a tunnel with the server so clients can connect and be routed.
func (t *TunnelService) StartTunnel(tun models.Tunnel) error {
	reg := tunnel.TunnelRegistration{
		TunnelID:      tun.ID.String(),
		Subdomain:     tun.Subdomain,
		MCLocalPort:   tun.MCLocalPort,
		HTTPLocalPort: tun.HTTPLocalPort,
		UDPLocalPort:  tun.UDPLocalPort,
		UDPPublicPort: tun.UDPPublicPort,
	}
	t.server.RegisterTunnel(reg)
	return nil
}

// StopTunnel removes the tunnel from active routing and disconnects the client.
func (t *TunnelService) StopTunnel(tun models.Tunnel) {
	t.server.UnregisterTunnel(tun.ID.String(), tun.Subdomain, tun.UDPPublicPort)
}

// IsClientConnected returns true if the VoidLink desktop app is connected for this tunnel.
func (t *TunnelService) IsClientConnected(tunnelID string) bool {
	return t.server.IsClientConnected(tunnelID)
}

// IsUDPPortInUse checks whether the given UDP public port is in use at the server level.
func (t *TunnelService) IsUDPPortInUse(port int) bool {
	return t.server.IsUDPPortInUse(port)
}

// RestoreActiveTunnels re-registers all is_active tunnels from the database
// into the server's in-memory routing tables. Call this once on startup.
func (t *TunnelService) RestoreActiveTunnels() {
	ctx := context.Background()
	rows, err := database.Pool.Query(ctx, `
		SELECT id, subdomain, mc_local_port, http_local_port, udp_local_port, udp_public_port
		FROM tunnels WHERE is_active = TRUE
	`)
	if err != nil {
		log.Printf("[TunnelService] Failed to restore active tunnels: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var tun models.Tunnel
		if err := rows.Scan(
			&tun.ID, &tun.Subdomain,
			&tun.MCLocalPort, &tun.HTTPLocalPort,
			&tun.UDPLocalPort, &tun.UDPPublicPort,
		); err != nil {
			log.Printf("[TunnelService] Failed to scan tunnel row: %v", err)
			continue
		}
		if err := t.StartTunnel(tun); err == nil {
			count++
		}
	}
	log.Printf("[TunnelService] Restored %d active tunnel(s)", count)
}
