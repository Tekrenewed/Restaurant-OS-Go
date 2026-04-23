package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"cloud.google.com/go/firestore"
)

// HandleUpdateOrder updates an order's fields in both Postgres and Firestore.
// PATCH /api/v1/orders/{id}
// Architecture: Postgres UPDATE (source of truth) → Firestore update (real-time sync)
//
// This replaces the frontend's direct Firestore updateDoc calls, ensuring
// both data stores stay in sync when KDS bumps an order or status changes.
func (s *Server) HandleUpdateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract order ID from URL: /api/v1/orders/{id}
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	orderID := ""
	for i, part := range parts {
		if part == "orders" && i+1 < len(parts) {
			orderID = parts[i+1]
			break
		}
	}
	if orderID == "" {
		http.Error(w, `{"error":"missing_order_id"}`, http.StatusBadRequest)
		return
	}

	var fields map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		http.Error(w, `{"error":"invalid_payload"}`, http.StatusBadRequest)
		return
	}

	// 1. Postgres UPDATE (source of truth)
	// Only update known columns to prevent SQL injection via arbitrary field names
	if s.DB != nil {
		if status, ok := fields["status"].(string); ok && status != "" {
			_, err := s.DB.Exec(`UPDATE orders SET status = $1 WHERE id = $2`, status, orderID)
			if err != nil {
				log.Printf("WARN: Postgres order status update failed for %s: %v", orderID, err)
			}
		}
		if isPaid, ok := fields["isPaid"].(bool); ok {
			paymentStatus := "unpaid"
			if isPaid {
				paymentStatus = "paid"
			}
			_, err := s.DB.Exec(`UPDATE orders SET payment_status = $1 WHERE id = $2`, paymentStatus, orderID)
			if err != nil {
				log.Printf("WARN: Postgres order payment update failed for %s: %v", orderID, err)
			}
		}
	}

	// 2. Firestore UPDATE (real-time cache — this triggers the onSnapshot in the frontend)
	tc := s.GetFirestoreForRequest(r)
	if tc != nil && tc.Firestore != nil {
		ctx := r.Context()
		_, err := tc.Firestore.Collection("orders").Doc(orderID).Set(ctx, fields, firestore.MergeAll)
		if err != nil {
			log.Printf("WARN: Firestore order update failed for %s: %v", orderID, err)
		}
	}

	log.Printf("[ORDER] Updated %s: %v", orderID, fields)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "updated", "id": orderID})
}
