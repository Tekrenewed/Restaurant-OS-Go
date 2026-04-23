package main

import (
	"log"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func main() {
	dsn := "postgres://postgres:26F4l00da26@8.228.48.20:5432/restaurant_os?sslmode=disable"
	db, err := sqlx.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping DB: %v", err)
	}

	query := `ALTER TABLE orders ADD COLUMN IF NOT EXISTS needs_printing BOOLEAN DEFAULT false;`
	_, err = db.Exec(query)
	if err != nil {
		log.Fatalf("Failed to run migration: %v", err)
	}

	log.Println("Migration successful: added needs_printing column to orders table.")
}
