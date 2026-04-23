package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

// DBWrapper encapsulates the database connection pool.
type DBWrapper struct {
	*sql.DB
}

// Config holds PostgreSQL connection parameters.
type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
}

// NewConnectionPool creates an enterprise-grade connection pool for Postgres.
// It includes timeouts and max connection limits to prevent microservices from starving the DB.
func NewConnectionPool(ctx context.Context, cfg Config) (*DBWrapper, error) {
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enterprise Connection Pool Settings
	db.SetMaxOpenConns(25)                 // Prevent Cloud SQL connection exhaustion
	db.SetMaxIdleConns(5)                  // Keep a few warm connections
	db.SetConnMaxLifetime(5 * time.Minute) // Prevent stale connections

	// Verify connection
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("Successfully established PostgreSQL connection pool")
	return &DBWrapper{DB: db}, nil
}
