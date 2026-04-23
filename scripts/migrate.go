package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"sort"
	"strings"

	"restaurant-os/pkg/database"

	"github.com/joho/godotenv"
)

func main() {
	// 1. Load Environment Variables
	err := godotenv.Load("../.env")
	if err != nil {
		log.Println("No .env file found, relying on system environment variables")
	}

	// 2. Initialize DB Connection
	cfg := database.Config{
		Host:     os.Getenv("DB_HOST"),
		Port:     os.Getenv("DB_PORT"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PASS"),
		DBName:   os.Getenv("DB_NAME"),
		SSLMode:  "disable", // Use 'require' in prod
	}

	if cfg.Host == "" {
		log.Fatal("DB_HOST is not set. Cannot run migrations.")
	}

	dbPool, err := database.NewConnectionPool(context.Background(), cfg)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer dbPool.Close()

	log.Println("Database connection established. Starting migrations...")

	// 3. Find and sort .sql files
	migrationsDir := "./sql"
	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		log.Fatalf("Failed to read migrations directory: %v", err)
	}

	var sqlFiles []string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".sql") {
			sqlFiles = append(sqlFiles, f.Name())
		}
	}
	sort.Strings(sqlFiles) // Ensure they run in order (001, 002, etc.)

	// 4. Execute each migration
	for _, file := range sqlFiles {
		log.Printf("Executing migration: %s", file)
		
		content, err := fs.ReadFile(os.DirFS(migrationsDir), file)
		if err != nil {
			log.Fatalf("Failed to read migration %s: %v", file, err)
		}

		// Execute the raw SQL
		_, err = dbPool.ExecContext(context.Background(), string(content))
		if err != nil {
			log.Fatalf("Migration %s failed: %v", file, err)
		}
		
		log.Printf("Migration %s completed successfully.", file)
	}

	fmt.Println("All migrations applied successfully!")
}
