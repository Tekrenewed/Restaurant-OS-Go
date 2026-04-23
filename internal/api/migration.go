package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"google.golang.org/api/iterator"
)

// HandleMigrateFirestoreCustomers is a one-time migration endpoint that syncs
// Firestore customer documents into the PostgreSQL customers table.
// This fixes the historical data split where some customers exist only in Firestore.
// POST /api/v1/internal/migrate-customers
// Protected by MigrateKeyMiddleware (X-Migrate-Key header).
func (s *Server) HandleMigrateFirestoreCustomers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.Firebase == nil || s.Firebase.Firestore == nil {
		http.Error(w, `{"error":"firestore_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	if s.DB == nil {
		http.Error(w, `{"error":"database_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	// Phones/patterns to skip (test data, simulations, owner testing)
	skipPhones := map[string]bool{
		"07886204038": true, // Owner testing
	}
	skipPrefixes := []string{"test", "sim", "demo", "fake", "000"}

	ctx := r.Context()
	iter := s.Firebase.Firestore.Collection(CUSTOMERS_COLLECTION).Documents(ctx)

	var migrated, skipped, failed, alreadyExists int

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("Migrate: Error iterating Firestore docs: %v", err)
			break
		}

		data := doc.Data()
		phone := doc.Ref.ID // Document ID is the normalised phone number
		
		// Skip test data
		normalPhone := normalisePhone(phone)
		if skipPhones[normalPhone] {
			skipped++
			continue
		}
		shouldSkip := false
		for _, prefix := range skipPrefixes {
			if strings.HasPrefix(strings.ToLower(normalPhone), prefix) {
				shouldSkip = true
				break
			}
		}
		if shouldSkip {
			skipped++
			continue
		}

		// Skip if phone is empty or too short to be real
		if len(normalPhone) < 5 {
			skipped++
			continue
		}

		// Extract fields
		email, _ := data["email"].(string)
		name, _ := data["name"].(string)
		if name == "" {
			name = "Valued Customer"
		}

		// Attempt upsert into PostgreSQL
		_, err = s.DB.Exec(`
			INSERT INTO customers (phone, email, name)
			VALUES ($1, $2, $3)
			ON CONFLICT (phone) DO UPDATE SET
				email = COALESCE(NULLIF(EXCLUDED.email, ''), customers.email),
				name = COALESCE(NULLIF(EXCLUDED.name, ''), NULLIF(EXCLUDED.name, 'Valued Customer'), customers.name)
		`, normalPhone, email, name)

		if err != nil {
			log.Printf("Migrate: Failed to upsert %s: %v", normalPhone, err)
			failed++
		} else {
			migrated++
		}
	}

	result := map[string]interface{}{
		"status":         "complete",
		"migrated":       migrated,
		"skipped":        skipped,
		"failed":         failed,
		"already_exists": alreadyExists,
	}

	log.Printf("Firestore→Postgres Migration: %d migrated, %d skipped, %d failed", migrated, skipped, failed)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
