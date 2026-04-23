package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// HandleDeductOrderStock automatically deducts ingredients for a completed order
// POST /api/v1/orders/:id/deduct-stock
func (s *Server) HandleDeductOrderStock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Invalid order ID", http.StatusBadRequest)
		return
	}
	orderID := parts[4]

	// 1. Get the store_id for this order
	var order struct {
		StoreID string `db:"store_id"`
	}
	if err := s.DB.Get(&order, `SELECT store_id FROM orders WHERE id = $1`, orderID); err != nil {
		log.Printf("Inventory: Failed to find order %s: %v", orderID, err)
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}

	// 2. Find all items in this order
	var items []struct {
		ProductID string `db:"product_id"`
		Qty       int    `db:"qty"` // Assuming we have quantity, or we count rows
	}
	// Note: using count for rows if qty isn't a column
	if err := s.DB.Select(&items, `SELECT product_id, count(*) as qty FROM order_items WHERE order_id = $1 GROUP BY product_id`, orderID); err != nil {
		log.Printf("Inventory: Failed to find items for order %s: %v", orderID, err)
		http.Error(w, "Failed to load order items", http.StatusInternalServerError)
		return
	}

	// 3. For each product, find its ingredients and deduct stock
	tx, err := s.DB.Beginx()
	if err != nil {
		http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	for _, item := range items {
		// Find ingredients required for this product
		var ingredients []struct {
			ItemID          string  `db:"item_id"`
			QuantityPerUnit float64 `db:"quantity_per_unit"`
		}
		if err := tx.Select(&ingredients, `SELECT item_id, quantity_per_unit FROM product_ingredients WHERE product_id = $1`, item.ProductID); err != nil {
			continue // Skip if no ingredients mapped
		}

		for _, ing := range ingredients {
			totalDeduction := ing.QuantityPerUnit * float64(item.Qty)

			// Deduct from inventory_items
			_, err := tx.Exec(`UPDATE inventory_items SET current_stock = current_stock - $1, last_updated = $2 WHERE id = $3 AND store_id = $4`,
				totalDeduction, time.Now(), ing.ItemID, order.StoreID)
			if err != nil {
				log.Printf("Inventory: Failed to deduct stock for item %s: %v", ing.ItemID, err)
				continue
			}

			// Log the movement
			movementID := uuid.New().String()
			_, err = tx.Exec(`INSERT INTO stock_movements (id, item_id, store_id, quantity, movement_type, notes) 
				VALUES ($1, $2, $3, $4, $5, $6)`,
				movementID, ing.ItemID, order.StoreID, totalDeduction, "usage", "Auto-deducted for order "+orderID)
			if err != nil {
				log.Printf("Inventory: Failed to log movement for item %s: %v", ing.ItemID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to commit inventory changes", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Inventory successfully deducted",
	})
}
