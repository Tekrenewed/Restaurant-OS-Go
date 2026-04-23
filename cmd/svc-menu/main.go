package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"restaurant-os/pkg/database"
)

type MenuApplication struct {
	DB *database.DBWrapper
}

type MappingRequest struct {
	TenantID          string `json:"tenant_id"`
	Platform          string `json:"platform"`
	ExternalItemID    string `json:"external_item_id"`
	InternalProductID string `json:"internal_product_id"`
}

func main() {
	// Load Environment Variables
	err := godotenv.Load("../.env")
	if err != nil {
		log.Println("No .env file found, falling back to system env")
	}

	port := os.Getenv("MENU_PORT")
	if port == "" {
		port = "8082" // Default port for svc-menu
	}

	// Connect to Database
	cfg := database.Config{
		Host:     os.Getenv("DB_HOST"),
		Port:     os.Getenv("DB_PORT"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PASS"),
		DBName:   os.Getenv("DB_NAME"),
		SSLMode:  "disable",
	}

	dbPool, err := database.NewConnectionPool(context.Background(), cfg)
	if err != nil {
		log.Fatalf("Fatal: Failed to connect to DB: %v", err)
	}
	defer dbPool.Close()

	app := &MenuApplication{
		DB: dbPool,
	}

	// Routes
	mux := http.NewServeMux()
	mux.HandleFunc("/api/menu/mappings", app.handleMappings)

	// Wrap with API Key Middleware and CORS
	handler := CORSMiddleware(APIKeyMiddleware(mux))

	log.Printf("svc-menu starting on port %s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

// CORSMiddleware adds basic CORS headers to support frontend requests
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, x-api-key")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// APIKeyMiddleware checks for x-api-key header
func APIKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedKey := os.Getenv("SVC_MENU_API_KEY")
		
		// If no key is set in environment, we enforce security by denying all
		if expectedKey == "" {
			log.Println("CRITICAL: SVC_MENU_API_KEY is not set. Denying all requests.")
			http.Error(w, "Internal Server Configuration Error", http.StatusInternalServerError)
			return
		}

		providedKey := r.Header.Get("x-api-key")
		if providedKey != expectedKey {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleMappings router for GET and POST
func (app *MenuApplication) handleMappings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		app.getMappings(w, r)
	case http.MethodPost:
		app.createMapping(w, r)
	case http.MethodDelete:
		app.deleteMapping(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (app *MenuApplication) getMappings(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	platform := r.URL.Query().Get("platform")

	if tenantID == "" || platform == "" {
		http.Error(w, "tenant_id and platform are required", http.StatusBadRequest)
		return
	}

	query := `
		SELECT id, tenant_id, platform, external_item_id, internal_product_id, created_at 
		FROM aggregator_menu_mappings 
		WHERE tenant_id = $1 AND platform = $2
	`

	rows, err := app.DB.QueryContext(r.Context(), query, tenantID, platform)
	if err != nil {
		log.Printf("Failed to query mappings: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var mappings []map[string]interface{}
	for rows.Next() {
		var id, tenant, plat, extID, intID, createdAt string
		if err := rows.Scan(&id, &tenant, &plat, &extID, &intID, &createdAt); err != nil {
			log.Printf("Failed to scan row: %v", err)
			continue
		}
		mappings = append(mappings, map[string]interface{}{
			"id":                  id,
			"tenant_id":           tenant,
			"platform":            plat,
			"external_item_id":    extID,
			"internal_product_id": intID,
			"created_at":          createdAt,
		})
	}

	// Prevent returning null JSON
	if mappings == nil {
		mappings = []map[string]interface{}{}
	}

	json.NewEncoder(w).Encode(mappings)
}

func (app *MenuApplication) createMapping(w http.ResponseWriter, r *http.Request) {
	var req MappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.TenantID == "" || req.Platform == "" || req.ExternalItemID == "" || req.InternalProductID == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	query := `
		INSERT INTO aggregator_menu_mappings (tenant_id, platform, external_item_id, internal_product_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, platform, external_item_id) 
		DO UPDATE SET internal_product_id = EXCLUDED.internal_product_id, updated_at = NOW()
		RETURNING id
	`

	var insertedID string
	err := app.DB.QueryRowContext(r.Context(), query, req.TenantID, req.Platform, req.ExternalItemID, req.InternalProductID).Scan(&insertedID)
	if err != nil {
		log.Printf("Failed to insert mapping: %v", err)
		http.Error(w, "Failed to save mapping", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": insertedID, "status": "success"})
}

func (app *MenuApplication) deleteMapping(w http.ResponseWriter, r *http.Request) {
	// For simplicity, passing ID via query param
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	query := `DELETE FROM aggregator_menu_mappings WHERE id = $1`
	result, err := app.DB.ExecContext(r.Context(), query, id)
	if err != nil {
		log.Printf("Failed to delete mapping: %v", err)
		http.Error(w, "Failed to delete mapping", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "Mapping not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}
