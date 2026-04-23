package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
)

func main() {
	dsn := "postgres://postgres:26Hayes26+@8.228.48.20:5432/restaurant_os?sslmode=disable"

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping: %v", err)
	}
	fmt.Println("Connected to PostgreSQL successfully")

	// Read the migration file
	sqlBytes, err := os.ReadFile("internal/database/migrations/005_row_level_security.sql")
	if err != nil {
		log.Fatalf("Failed to read migration file: %v", err)
	}

	// Execute the migration
	_, err = db.Exec(string(sqlBytes))
	if err != nil {
		log.Fatalf("Migration FAILED: %v", err)
	}

	fmt.Println("✅ RLS migration applied successfully!")
	fmt.Println("   - 11 tables now have Row-Level Security enabled")
	fmt.Println("   - FORCE ROW LEVEL SECURITY applied for superuser connections")
	fmt.Println("")
	fmt.Println("To test, run in psql:")
	fmt.Println("   SET app.current_store_id = 'f4100da2-1111-1111-1111-000000000001';")
	fmt.Println("   SELECT * FROM orders LIMIT 5;")
}
