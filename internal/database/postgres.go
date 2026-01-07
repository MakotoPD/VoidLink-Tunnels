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
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("failed to parse database URL: %w", err)
	}

	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

	// Initial connection attempt
	ctx := context.Background() // Use background for retries
	
	// Helper to try connecting
	connect := func(cfg *pgxpool.Config) (*pgxpool.Pool, error) {
		ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return pgxpool.NewWithConfig(ctxTimeout, cfg)
	}

	Pool, err = connect(config)
	if err == nil {
		// Ping to verify
		ctxPing, cancelPing := context.WithTimeout(ctx, 5*time.Second)
		defer cancelPing()
		if err = Pool.Ping(ctxPing); err == nil {
			log.Println("✓ Connected to PostgreSQL")
			return nil
		}
		Pool.Close() // Close successful pool if ping failed
	}

	// Check if error is "database does not exist"
	if err != nil && strings.Contains(err.Error(), "does not exist") {
		log.Printf("⚠️ Target database does not exist. Attempting to create it...")
		
		// Parse URL to get DB name and switch to 'postgres' DB
		u, uErr := url.Parse(databaseURL)
		if uErr != nil {
			return fmt.Errorf("failed to parse URL for recovery: %w", uErr)
		}
		
		targetDB := strings.TrimPrefix(u.Path, "/")
		u.Path = "/postgres"
		postgresURL := u.String()

		postgresConfig, pErr := pgxpool.ParseConfig(postgresURL)
		if pErr != nil {
			return fmt.Errorf("failed to parse postgres defaults URL: %w", pErr)
		}

		// Connect to 'postgres' DB
		pgPool, connErr := connect(postgresConfig)
		if connErr != nil {
			return fmt.Errorf("failed to connect to default postgres DB: %w (original error: %v)", connErr, err)
		}
		defer pgPool.Close()

		// Create database
		_, createErr := pgPool.Exec(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, targetDB))
		if createErr != nil {
			// Ignore if it somehow exists now
			if !strings.Contains(createErr.Error(), "already exists") {
				return fmt.Errorf("failed to create database %s: %w", targetDB, createErr)
			}
		}
		log.Printf("✓ Created database: %s", targetDB)

		// Retry connection to target DB
		Pool, err = connect(config)
		if err != nil {
			return fmt.Errorf("failed to connect to new database: %w", err)
		}
		
		log.Println("✓ Connected to PostgreSQL (after creation)")
		return nil
	}

	return fmt.Errorf("failed to connect to database: %w", err)
}

func Close() {
	if Pool != nil {
		Pool.Close()
		log.Println("✓ Database connection closed")
	}
}
