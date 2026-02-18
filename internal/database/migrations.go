package database

import (
	"context"
	"log"
)

func RunMigrations() error {
	ctx := context.Background()

	migrations := []string{
		// Users
		`CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			email VARCHAR(255) UNIQUE NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			totp_secret VARCHAR(64),
			totp_enabled BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// Tunnels — new simplified schema:
		//   mc_local_port  : which local port the Minecraft server listens on
		//   http_local_port: local HTTP port for web map (NULL = disabled)
		//   udp_local_port : local voice chat UDP port
		//   udp_public_port: allocated public UDP port (stable, unique)
		`CREATE TABLE IF NOT EXISTS tunnels (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(100) NOT NULL,
			subdomain VARCHAR(50) UNIQUE NOT NULL,
			region VARCHAR(10) DEFAULT 'eu',
			is_active BOOLEAN DEFAULT FALSE,
			mc_local_port INT NOT NULL DEFAULT 25565,
			http_local_port INT DEFAULT NULL,
			udp_local_port INT NOT NULL DEFAULT 24454,
			udp_public_port INT UNIQUE DEFAULT NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
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
		`CREATE INDEX IF NOT EXISTS idx_tunnels_subdomain ON tunnels(subdomain)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash ON refresh_tokens(token_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_hash ON password_reset_tokens(token_hash)`,

		// Migration: add new columns if upgrading from old schema
		`ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS mc_local_port INT NOT NULL DEFAULT 25565`,
		`ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS http_local_port INT DEFAULT NULL`,
		`ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS udp_local_port INT NOT NULL DEFAULT 24454`,
		`ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS udp_public_port INT UNIQUE DEFAULT NULL`,

		// Migration: drop old columns/tables if upgrading
		`DROP TABLE IF EXISTS tunnel_ports`,
		`ALTER TABLE tunnels DROP COLUMN IF EXISTS frp_run_id`,
	}

	for i, migration := range migrations {
		_, err := Pool.Exec(ctx, migration)
		if err != nil {
			log.Printf("Migration %d failed: %v", i+1, err)
			return err
		}
	}

	log.Println("Database migrations completed")
	return nil
}
