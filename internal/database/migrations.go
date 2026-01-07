package database

import (
	"context"
	"log"
)

func RunMigrations() error {
	ctx := context.Background()

	migrations := []string{
		// Users table
		`CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			email VARCHAR(255) UNIQUE NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			totp_secret VARCHAR(64),
			totp_enabled BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// Tunnels table
		`CREATE TABLE IF NOT EXISTS tunnels (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(100) NOT NULL,
			subdomain VARCHAR(50) UNIQUE NOT NULL,
			region VARCHAR(10) DEFAULT 'eu',
			is_active BOOLEAN DEFAULT FALSE,
			frp_run_id VARCHAR(100),
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// Tunnel ports
		`CREATE TABLE IF NOT EXISTS tunnel_ports (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			tunnel_id UUID NOT NULL REFERENCES tunnels(id) ON DELETE CASCADE,
			label VARCHAR(50) NOT NULL,
			local_port INT NOT NULL,
			public_port INT NOT NULL,
			protocol VARCHAR(10) DEFAULT 'tcp',
			UNIQUE(tunnel_id, local_port),
			UNIQUE(public_port)
		)`,

		// Refresh tokens
		`CREATE TABLE IF NOT EXISTS refresh_tokens (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash VARCHAR(255) NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		)`,

		// Password reset tokens
		`CREATE TABLE IF NOT EXISTS password_reset_tokens (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash VARCHAR(255) NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			used BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT NOW()
		)`,

		// Indexes
		`CREATE INDEX IF NOT EXISTS idx_tunnels_user_id ON tunnels(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tunnel_ports_tunnel_id ON tunnel_ports(tunnel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash ON refresh_tokens(token_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_hash ON password_reset_tokens(token_hash)`,
	}

	for i, migration := range migrations {
		_, err := Pool.Exec(ctx, migration)
		if err != nil {
			log.Printf("Migration %d failed: %v", i+1, err)
			return err
		}
	}

	log.Println("✓ Database migrations completed")
	return nil
}
