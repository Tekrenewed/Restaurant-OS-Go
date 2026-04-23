package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"cloud.google.com/go/firestore"
	"github.com/jmoiron/sqlx"
)

type Customer struct {
	ID            string  `json:"id" db:"id"`
	ShortID       string  `json:"short_id" db:"short_id"`
	StoreID       *string `json:"store_id" db:"store_id"`
	Phone         string  `json:"phone" db:"phone"`
	Email         string  `json:"email" db:"email"`
	Name          string  `json:"name" db:"name"`
	LoyaltyPoints int     `json:"loyalty_points" db:"loyalty_points"`
	CreatedAt     string  `json:"created_at" db:"created_at"`
}

// HandleGetCustomer handles looking up a customer by short_id, phone, or email
func HandleGetCustomer(db *sqlx.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			http.Error(w, "Query parameter 'q' is required", http.StatusBadRequest)
			return
		}

		var customer Customer
		// Search by short_id, phone, or email
		err := db.Get(&customer, `
			SELECT id, short_id, store_id, phone, email, name, loyalty_points, created_at 
			FROM customers 
			WHERE short_id = $1 OR phone = $1 OR email = $1 LIMIT 1
		`, query)

		if err != nil {
			http.Error(w, "Customer not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"customer": customer})
	}
}

// HandleCreateCustomer registers a new customer.
// Architecture: Postgres INSERT (source of truth) → Firestore sync (real-time CRM)
// Accepts legacy function-param DB call from main.go AND works as a Server method.
func HandleCreateCustomer(db *sqlx.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Phone    string `json:"phone"`
			Email    string `json:"email"`
			Name     string `json:"name"`
			StoreID  string `json:"store_id"`
			Source   string `json:"source"`
			WaiterID string `json:"waiterId"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if db == nil {
			http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
			return
		}

		// 1. Postgres INSERT (source of truth)
		var newCustomer Customer
		err := db.QueryRowx(`
			INSERT INTO customers (phone, email, name, store_id)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (phone) DO UPDATE SET 
				email = COALESCE(NULLIF(EXCLUDED.email, ''), customers.email),
				name = COALESCE(NULLIF(EXCLUDED.name, ''), customers.name),
				store_id = COALESCE(EXCLUDED.store_id, customers.store_id)
			RETURNING id, short_id, store_id, phone, email, name, loyalty_points, created_at
		`, req.Phone, req.Email, req.Name, nilIfEmpty(req.StoreID)).StructScan(&newCustomer)

		if err != nil {
			log.Printf("ERROR: Postgres customer create failed: %v", err)
			http.Error(w, "Failed to create customer: "+err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[CRM] Customer upserted in Postgres: %s (%s) source=%s", req.Name, newCustomer.ID, req.Source)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"customer": newCustomer})
	}
}

// HandleCreateCustomerWithSync is the Server method version that also syncs to Firestore.
// Used by routes that need the full dual-write pattern.
func (s *Server) HandleCreateCustomerWithSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone    string `json:"phone"`
		Email    string `json:"email"`
		Name     string `json:"name"`
		StoreID  string `json:"store_id"`
		Source   string `json:"source"`
		WaiterID string `json:"waiterId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	storeID := req.StoreID
	if storeID == "" {
		storeID = r.Header.Get("X-Store-ID")
	}

	// Dual-write: Postgres → Firestore
	tc := s.GetFirestoreForRequest(r)
	var fsClient *firestore.Client
	if tc != nil {
		fsClient = tc.Firestore
	}
	customerID := UpsertCustomerInternal(s.DB, fsClient, ctx, storeID, req.Phone, req.Email, req.Name)

	log.Printf("[CRM] Customer upserted (dual-write): %s (%s) source=%s waiter=%s", req.Name, customerID, req.Source, req.WaiterID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "created", "id": customerID})
}

// UpsertCustomerInternal silently ensures a customer profile exists in BOTH
// PostgreSQL (source of truth) AND Firestore (real-time loyalty engine).
// Used by checkout traps and POS silent capture.
// Returns the customer's Postgres UUID (for linking to orders).
func UpsertCustomerInternal(db *sqlx.DB, fsClient *firestore.Client, ctx context.Context, storeID, phone, email, name string) string {
	if phone == "" && email == "" {
		return ""
	}
	if name == "" {
		name = "Valued Customer"
	}
	phone = normalisePhone(phone)

	// 1. Upsert into PostgreSQL (source of truth)
	var customerID string
	err := db.QueryRow(`
		INSERT INTO customers (phone, email, name, store_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (phone) DO UPDATE SET 
			email = COALESCE(NULLIF(EXCLUDED.email, ''), customers.email),
			name = COALESCE(NULLIF(EXCLUDED.name, ''), customers.name),
			store_id = COALESCE(EXCLUDED.store_id, customers.store_id)
		RETURNING id
	`, phone, email, name, nilIfEmpty(storeID)).Scan(&customerID)

	if err != nil {
		log.Printf("UpsertCustomerInternal: Postgres upsert failed for %s: %v", phone, err)
		// Try to fetch existing ID as fallback
		_ = db.Get(&customerID, `SELECT id FROM customers WHERE phone = $1 LIMIT 1`, phone)
	}

	// 2. Also ensure the Firestore doc exists (for real-time loyalty engine)
	if fsClient != nil && phone != "" {
		docRef := fsClient.Collection(CUSTOMERS_COLLECTION).Doc(phone)
		snap, err := docRef.Get(ctx)
		if err != nil || !snap.Exists() {
			// Create the Firestore doc with initial data
			_, err = docRef.Set(ctx, map[string]interface{}{
				"phone":          phone,
				"email":          email,
				"name":           name,
				"totalOrders":    0,
				"totalSpent":     0.0,
				"categoryCounts": map[string]interface{}{},
				"rewards":        []interface{}{},
				"storeId":        storeID,
			})
			if err != nil {
				log.Printf("UpsertCustomerInternal: Firestore create failed for %s: %v", phone, err)
			} else {
				log.Printf("UpsertCustomerInternal: Created Firestore doc for %s", phone)
			}
		} else {
			// Doc exists — update email/name if they were empty
			updates := []firestore.Update{}
			data := snap.Data()
			if existingEmail, _ := data["email"].(string); existingEmail == "" && email != "" {
				updates = append(updates, firestore.Update{Path: "email", Value: email})
			}
			if existingName, _ := data["name"].(string); (existingName == "" || existingName == "Valued Customer") && name != "" && name != "Valued Customer" {
				updates = append(updates, firestore.Update{Path: "name", Value: name})
			}
			if len(updates) > 0 {
				_, _ = docRef.Update(ctx, updates)
			}
		}
	}

	return customerID
}

// nilIfEmpty returns nil for empty strings (for nullable UUID columns)
func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
