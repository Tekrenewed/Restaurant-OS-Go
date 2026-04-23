package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"restaurant-os/internal/models"
)

// ProductResponse maps to the global catalog of products
type ProductResponse struct {
	ID        string  `json:"id" db:"id"`
	BrandID   string  `json:"brand_id" db:"brand_id"`
	Name      string  `json:"name" db:"name"`
	Category  string  `json:"category" db:"category"`
	BasePrice float64 `json:"base_price" db:"base_price"`
	Is86d     bool    `json:"is_86d" db:"is_86d"`
}

// StoreMenuResponse maps the specific price overrides and availability for a store and channel
type StoreMenuResponse struct {
	ProductID   string  `json:"product_id" db:"product_id"`
	ProductName string  `json:"product_name" db:"product_name"`
	Category    string  `json:"category" db:"category"`
	ChannelName string  `json:"channel_name" db:"channel_name"` // e.g. "Dine-In", "UberEats"
	FinalPrice  float64 `json:"final_price" db:"final_price"`
	IsActive    bool    `json:"is_active" db:"active_status"`
	Is86d       bool    `json:"is_86d" db:"is_86d"`
}

// HandleGetCatalog returns all products across all brands (Universal Menu)
func (s *Server) HandleGetCatalog(w http.ResponseWriter, r *http.Request) {
	if s.DB == nil {
		// Full mock menu for local dev (matches production seed)
		mock := []ProductResponse{
			// Faloodas
			{ID: "f001", BrandID: "FaloodaAndCo", Name: "Rose Falooda", Category: "Dessert", BasePrice: 5.49, Is86d: false},
			{ID: "f002", BrandID: "FaloodaAndCo", Name: "Pistachio Royale Falooda", Category: "Dessert", BasePrice: 5.49, Is86d: false},
			{ID: "f003", BrandID: "FaloodaAndCo", Name: "The Royal Heritage", Category: "Dessert", BasePrice: 4.99, Is86d: false},
			{ID: "f004", BrandID: "FaloodaAndCo", Name: "The Golden Monsoon", Category: "Dessert", BasePrice: 4.99, Is86d: false},
			{ID: "f005", BrandID: "FaloodaAndCo", Name: "The Salted Sunset", Category: "Dessert", BasePrice: 6.49, Is86d: false},
			// Chaats
			{ID: "c001", BrandID: "FaloodaAndCo", Name: "Samosa Chaat", Category: "Grill", BasePrice: 6.49, Is86d: false},
			{ID: "c002", BrandID: "FaloodaAndCo", Name: "Papdi Chaat", Category: "Grill", BasePrice: 5.99, Is86d: false},
			{ID: "c003", BrandID: "FaloodaAndCo", Name: "Dahi Bhalla", Category: "Grill", BasePrice: 5.99, Is86d: false},
			{ID: "c004", BrandID: "FaloodaAndCo", Name: "Aloo Tikki Chaat", Category: "Grill", BasePrice: 5.99, Is86d: false},
			{ID: "c005", BrandID: "FaloodaAndCo", Name: "Fruit Chaat", Category: "Grill", BasePrice: 6.99, Is86d: false},
			{ID: "c006", BrandID: "FaloodaAndCo", Name: "Falooda & Co Special Chaat", Category: "Grill", BasePrice: 8.49, Is86d: false},
			{ID: "c007", BrandID: "FaloodaAndCo", Name: "Mixed Chaat", Category: "Grill", BasePrice: 5.99, Is86d: false},
			// Drinks
			{ID: "d001", BrandID: "FaloodaAndCo", Name: "KADAK CHAI", Category: "Drinks", BasePrice: 2.75, Is86d: false},
			{ID: "d002", BrandID: "FaloodaAndCo", Name: "Pink Kashmiri Chai", Category: "Drinks", BasePrice: 3.99, Is86d: false},
			{ID: "d003", BrandID: "FaloodaAndCo", Name: "Mango Lassi", Category: "Drinks", BasePrice: 4.99, Is86d: false},
			// Desserts (non-Falooda)
			{ID: "s001", BrandID: "FaloodaAndCo", Name: "Cookie Dough", Category: "Dessert", BasePrice: 6.75, Is86d: false},
			{ID: "s002", BrandID: "FaloodaAndCo", Name: "Tres Leches Cake", Category: "Dessert", BasePrice: 6.50, Is86d: false},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mock)
		return
	}

	var catalog []ProductResponse
	// We'll join against product_prices for the default Dine-In pricing (price_level_id = 1) assuming 1 is Dine-In
	query := `
		SELECT 
			p.id, 
			p.brand_id, 
			p.name, 
			p.category, 
			p.is_86d,
			COALESCE(MAX(pp.price_amount), 0) as base_price 
		FROM products p
		LEFT JOIN product_prices pp ON p.id = pp.product_id
		GROUP BY p.id, p.brand_id, p.name, p.category, p.is_86d
		ORDER BY p.category, p.name
	`
	
	if err := s.DB.Select(&catalog, query); err != nil {
		log.Printf("Failed to fetch catalog: %v", err)
		http.Error(w, `{"error":"server_error","message":"Failed to fetch catalog"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(catalog)
}

// HandleGetStoreMenu returns the channel-specific pricing and availability for a specific store
func (s *Server) HandleGetStoreMenu(w http.ResponseWriter, r *http.Request) {
	// Extract storeID from URL path, e.g., /api/v1/stores/{id}/menu
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Invalid store ID", http.StatusBadRequest)
		return
	}
	
	// Ensure strict UUID format to prevent DB syntax errors
	if _, err := uuid.Parse(parts[4]); err != nil {
		http.Error(w, "Invalid store ID format", http.StatusBadRequest)
		return
	}
	storeID := parts[4]

	if s.DB == nil {
		// Mock response if no DB attached
		mock := []StoreMenuResponse{
			{ProductID: "1", ProductName: "Chicken Tikka Masala", Category: "Curry", ChannelName: "Dine-In", FinalPrice: 10.99, IsActive: true, Is86d: false},
			{ProductID: "1", ProductName: "Chicken Tikka Masala", Category: "Curry", ChannelName: "UberEats", FinalPrice: 13.99, IsActive: true, Is86d: false},
			{ProductID: "3", ProductName: "Mango Falooda", Category: "Dessert", ChannelName: "Dine-In", FinalPrice: 6.99, IsActive: true, Is86d: false},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mock)
		return
	}

	// CTE query to merge base products with store-specific price overrides
	query := `
		WITH StoreMenu AS (
			SELECT 
				p.id AS product_id,
				p.name AS product_name,
				p.category,
				p.is_86d,
				pl.name AS channel_name,
				COALESCE(pp.price_amount, 0) AS final_price,  -- Assume pp has actual price override
				COALESCE(pp.is_active, true) AS active_status
			FROM products p
			CROSS JOIN price_levels pl
			LEFT JOIN product_prices pp ON p.id = pp.product_id 
				AND pp.store_id = $1 
				AND pl.id = pp.price_level_id
			WHERE p.brand_id = (SELECT brand_id FROM stores WHERE id = $1)
		)
		SELECT * FROM StoreMenu ORDER BY category, product_name;
	`

	var menu []StoreMenuResponse
	if err := s.DB.Select(&menu, query, storeID); err != nil {
		log.Printf("Failed to fetch store menu: %v", err)
		http.Error(w, `{"error":"server_error","message":"Failed to fetch store menu"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(menu)
}

// HandleGetStoreTables returns all tables for a store
func (s *Server) HandleGetStoreTables(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Invalid store ID", http.StatusBadRequest)
		return
	}
	
	if _, err := uuid.Parse(parts[4]); err != nil {
		http.Error(w, "Invalid store ID format", http.StatusBadRequest)
		return
	}
	storeID := parts[4]

	query := `SELECT id, store_id, table_number, is_active FROM tables WHERE store_id = $1 ORDER BY table_number ASC`
	var tables []models.Table
	if s.DB != nil {
		if err := s.DB.Select(&tables, query, storeID); err != nil {
			log.Printf("Failed to fetch tables: %v", err)
			http.Error(w, `{"error":"server_error","message":"Failed to fetch tables"}`, http.StatusInternalServerError)
			return
		}
	} else {
		// Mock for testing
		tables = []models.Table{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tables)
}

// Handle86Product toggles the is_86d flag for a product
func (s *Server) Handle86Product(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Invalid product ID", http.StatusBadRequest)
		return
	}
	productID := parts[4]

	var req struct {
		Is86d bool `json:"is_86d"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if s.DB != nil {
		_, err := s.DB.Exec(`UPDATE products SET is_86d = $1 WHERE id = $2`, req.Is86d, productID)
		if err != nil {
			log.Printf("Failed to update product status: %v", err)
			http.Error(w, `{"error":"server_error","message":"Failed to update product"}`, http.StatusInternalServerError)
			return
		}

		// Also update Firestore to instantly reflect on UI? (Maybe overkill due to Next.js API, 
		// but since we want the KDS toggles to hide it on the website instantly,
		// we should fetch the menu and update state, or website pulls menu fresh.)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
