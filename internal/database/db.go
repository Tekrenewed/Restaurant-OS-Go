package database

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// InitDB initializes the database connection with retry logic for Cloud Run
func InitDB() *sqlx.DB {
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		log.Println("[DB] DATABASE_URL not set, running without database")
		return nil
	}

	// Log a masked version for debugging (show host, hide password)
	maskedDSN := dsn
	if idx := strings.Index(dsn, "password="); idx >= 0 {
		end := strings.Index(dsn[idx:], " ")
		if end > 0 {
			maskedDSN = dsn[:idx] + "password=***" + dsn[idx+end:]
		}
	}
	log.Printf("[DB] DATABASE_URL present (len=%d): %s", len(dsn), maskedDSN)

	// Auto-detect Cloud Run environment and construct Cloud SQL Unix socket DSN
	// if the current DSN points to localhost (development value)
	if strings.Contains(dsn, "localhost") || strings.Contains(dsn, "127.0.0.1") {
		instanceConn := os.Getenv("INSTANCE_CONNECTION_NAME")
		if instanceConn == "" {
			// Try to infer from Cloud Run's Cloud SQL annotation
			// Cloud Run mounts the socket at /cloudsql/<connection-name>
			instanceConn = "faloodaandco:europe-west2:restaurant-os-db"
		}
		socketDir := "/cloudsql"
		dbUser := "postgres"
		dbName := "restaurant_os"
		dsn = fmt.Sprintf("host=%s/%s user=%s dbname=%s sslmode=disable",
			socketDir, instanceConn, dbUser, dbName)
		log.Printf("[DB] Cloud SQL Unix socket mode: connecting via %s/%s", socketDir, instanceConn)
	}

	var db *sqlx.DB
	var err error

	// Retry up to 5 times — Cloud SQL proxy socket may not be ready immediately
	for i := 0; i < 5; i++ {
		db, err = sqlx.Open("postgres", dsn)
		if err != nil {
			log.Printf("[DB] Attempt %d/5: Could not open DB: %v", i+1, err)
			time.Sleep(2 * time.Second)
			continue
		}

		// Configure connection pool
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)

		// Test the connection
		if err = db.Ping(); err != nil {
			log.Printf("[DB] Attempt %d/5: Ping failed: %v", i+1, err)
			db.Close()
			time.Sleep(2 * time.Second)
			continue
		}

		log.Println("[DB] ✅ Connected to PostgreSQL successfully")
		return db
	}

	// Don't crash the server — run without DB and log the error
	log.Printf("[DB] ❌ FATAL: Could not connect to PostgreSQL after 5 attempts: %v (running without database)", err)
	return nil
}
