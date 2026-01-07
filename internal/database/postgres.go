package database

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func Connect(databaseURL string) error {
	// 1. Ensure database exists
	if err := ensureDatabase(databaseURL); err != nil {
		log.Printf("⚠️ Failed to ensure database exists: %v. Continuing strictly with provided URL...", err)
	}

	// 2. Parse config for main connection
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("failed to parse database URL: %w", err)
	}

	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 3. Connect to target database
	Pool, err = pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// 4. Test connection
	if err := Pool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("✓ Connected to PostgreSQL")
	return nil
}

// ensureDatabase connects to the default 'postgres' database to check for and create the target database if missing.
func ensureDatabase(connString string) error {
	// Parse the connection string
	u, err := url.Parse(connString)
	if err != nil {
		return err
	}

	targetDB := strings.TrimPrefix(u.Path, "/")
	if targetDB == "" || targetDB == "postgres" {
		return nil // Nothing to do
	}

	// Switch to 'postgres' database for maintenance
	u.Path = "/postgres"
	maintenanceURL := u.String()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Connect to maintenance DB
	// We use a temporary pool just for this operation
	cfg, err := pgxpool.ParseConfig(maintenanceURL)
	if err != nil {
		return err
	}
	
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to maintenance db: %w", err)
	}
	defer pool.Close()

	// Check if target database exists
	var exists bool
	err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", targetDB).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check database existence: %w", err)
	}

	if !exists {
		log.Printf("Database '%s' does not exist. Creating...", targetDB)
		// CREATE DATABASE cannot run in a transaction block, so we use Exec directly
		_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, targetDB))
		if err != nil {
			return fmt.Errorf("failed to create database: %w", err)
		}
		log.Printf("✓ Created database '%s'", targetDB)
	}

	return nil
}

func Close() {
	if Pool != nil {
		Pool.Close()
		log.Println("✓ Database connection closed")
	}
}
