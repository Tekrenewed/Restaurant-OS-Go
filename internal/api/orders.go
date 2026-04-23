package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"restaurant-os/internal/integrations"
	"restaurant-os/internal/models"

	"github.com/google/uuid"
)

// OrderListItem is the response shape for listing active orders
type OrderListItem struct {
	ID            string   `json:"id" db:"id"`
	StoreID       string   `json:"store_id" db:"store_id"`
	Source        string   `json:"source" db:"order_source"`
	NetTotal      float64  `json:"net_total" db:"net_total"`
	VATTotal      float64  `json:"vat_total" db:"vat_total"`
	ServiceCharge float64  `json:"service_charge" db:"service_charge"`
	GrossTotal    float64  `json:"gross_total" db:"gross_total"`
	Status        string   `json:"status" db:"status"`
	CreatedAt     string   `json:"created_at" db:"created_at"`
	TableNumber   *int     `json:"table_number" db:"table_number"`
	CustomerName  *string  `json:"customer_name" db:"customer_name"`
	CustomerPhone *string  `json:"customer_phone" db:"customer_phone"`
	PaymentStatus *string  `json:"payment_status" db:"payment_status"`
	PaymentMethod *string  `json:"payment_method" db:"payment_method"`
}

// OrderItemRow is a single item from the order_items table
type OrderItemRow struct {
	ID        string  `json:"id" db:"id"`
	OrderID   string  `json:"order_id" db:"order_id"`
	ProductID string  `json:"product_id" db:"product_id"`
	Name      string  `json:"name" db:"name"`
	PricePaid float64 `json:"price_paid" db:"price_paid"`
}

// OrderWithItems combines an order with its items for the frontend
type OrderWithItems struct {
	OrderListItem
	Items []OrderItemRow `json:"items"`
}

// HandleGetOrders returns active orders for a store, optionally filtered by status.
// GET /api/v1/stores/:id/orders?status=pending,preparing,ready
func (s *Server) HandleGetOrders(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Invalid store ID", http.StatusBadRequest)
		return
	}
	storeID := parts[4]

	if s.DB == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]OrderWithItems{})
		return
	}

	// Parse optional status filter
	statusFilter := r.URL.Query().Get("status")
	var statusList []string
	if statusFilter != "" {
		statusList = strings.Split(statusFilter, ",")
	}

	// Build query
	query := `SELECT id, store_id, order_source, net_total, vat_total, service_charge, gross_total, status, created_at, table_number, customer_name, customer_phone, payment_status, payment_method
		FROM orders WHERE store_id = $1`
	args := []interface{}{storeID}

	if len(statusList) > 0 {
		placeholders := make([]string, len(statusList))
		for i, s := range statusList {
			args = append(args, strings.TrimSpace(s))
			placeholders[i] = "$" + string(rune('0'+len(args)))
		}
		// Use a simpler approach with ANY
		query += ` AND status = ANY($2::text[])`
		args = []interface{}{storeID, statusList}
	}

	query += ` ORDER BY created_at DESC LIMIT 50`

	var orders []OrderListItem

	if len(statusList) > 0 {
		// Use the array approach
		arrayQuery := `SELECT id, store_id, order_source, net_total, vat_total, service_charge, gross_total, status, created_at, table_number, customer_name, customer_phone, payment_status, payment_method
			FROM orders WHERE store_id = $1 AND status = ANY($2::text[]) ORDER BY created_at DESC LIMIT 50`
		if err := s.DB.Select(&orders, arrayQuery, storeID, statusList); err != nil {
			log.Printf("Failed to fetch orders: %v", err)
			http.Error(w, `{"error":"server_error","message":"Failed to fetch orders"}`, http.StatusInternalServerError)
			return
		}
	} else {
		simpleQuery := `SELECT id, store_id, order_source, net_total, vat_total, service_charge, gross_total, status, created_at, table_number, customer_name, customer_phone, payment_status, payment_method
			FROM orders WHERE store_id = $1 ORDER BY created_at DESC LIMIT 50`
		if err := s.DB.Select(&orders, simpleQuery, storeID); err != nil {
			log.Printf("Failed to fetch orders: %v", err)
			http.Error(w, `{"error":"server_error","message":"Failed to fetch orders"}`, http.StatusInternalServerError)
			return
		}
	}

	// Fetch items for each order
	result := make([]OrderWithItems, len(orders))
	for i, order := range orders {
		var items []OrderItemRow
		itemQuery := `SELECT id, order_id, product_id, name, price_paid FROM order_items WHERE order_id = $1`
		if err := s.DB.Select(&items, itemQuery, order.ID); err != nil {
			log.Printf("Failed to fetch items for order %s: %v", order.ID, err)
			items = []OrderItemRow{}
		}
		result[i] = OrderWithItems{
			OrderListItem: order,
			Items:         items,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// HandleUpdateOrderStatus updates the status of an order in both PostgreSQL and Firestore.
// PATCH /api/v1/orders/:id/status
func (s *Server) HandleUpdateOrderStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract order ID: /api/v1/orders/{id}/status
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Invalid order ID", http.StatusBadRequest)
		return
	}
	orderID := parts[4]

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	// Validate status
	validStatuses := map[string]bool{
		"pending": true, "kitchen": true, "preparing": true,
		"ready": true, "completed": true, "cancelled": true,
		"refunded": true, "paid": true,
	}
	if !validStatuses[req.Status] {
		http.Error(w, "Invalid status: "+req.Status, http.StatusBadRequest)
		return
	}

	// 1. Update PostgreSQL (source of truth)
	if s.DB != nil {
		updateQuery := `UPDATE orders SET status = $1 WHERE id = $2`
		if req.Status == "completed" || req.Status == "cancelled" {
			updateQuery = `UPDATE orders SET status = $1, completed_at = $3 WHERE id = $2`
			_, err := s.DB.Exec(updateQuery, req.Status, orderID, time.Now())
			if err != nil {
				log.Printf("Failed to update order status: %v", err)
				http.Error(w, `{"error":"server_error","message":"Failed to update order"}`, http.StatusInternalServerError)
				return
			}
		} else {
			_, err := s.DB.Exec(updateQuery, req.Status, orderID)
			if err != nil {
				log.Printf("Failed to update order status: %v", err)
				http.Error(w, `{"error":"server_error","message":"Failed to update order"}`, http.StatusInternalServerError)
				return
			}
		}
	}

	// 2. Update Firestore for real-time sync
	if s.Firebase != nil {
		if req.Status == "completed" || req.Status == "cancelled" {
			// Remove from active_orders — clears KDS/POS screens
			s.Firebase.RemoveActiveOrder(r.Context(), orderID)
		} else {
			// Update status in place — KDS shows new status badge
			s.Firebase.UpdateOrderStatus(r.Context(), orderID, req.Status)
		}
		
		// If order is ready, send push notification
		if req.Status == "ready" {
			go func(ctx context.Context, id string) {
				docSnap, err := s.Firebase.Firestore.Collection("orders").Doc(id).Get(ctx)
				if err == nil && docSnap.Exists() {
					if token, ok := docSnap.Data()["fcmToken"].(string); ok && token != "" {
						s.Firebase.SendOrderReadyPush(ctx, token, id)
					}
				}
			}(context.Background(), orderID)
		}
	}

	// 3. On completion → send Google Review prompt email (non-blocking)
	tc := s.GetFirestoreForRequest(r)
	if req.Status == "completed" && tc != nil && tc.Firestore != nil {
		go s.SendReviewPromptEmail(context.Background(), tc.Firestore, orderID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "success",
		"order_id": orderID,
		"new_status": req.Status,
	})
}

// HandleCreatePayment initiates a payment for an order.
// POST /api/v1/orders/:id/pay
//
// When method is "dojo": Calls Dojo Cloud API to wake the physical terminal.
// The order is marked as "awaiting_payment" until the Dojo webhook confirms.
// When method is "cash" or "card": Records payment immediately (manual mode).
func (s *Server) HandleCreatePayment(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Method  string `json:"method"`   // "dojo", "card", or "cash"
		StoreID string `json:"store_id"` // Needed to look up the terminal
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if req.Method == "" {
		req.Method = "card"
	}

	// ─── DOJO AUTOMATED PAYMENT ───
	if req.Method == "dojo" {
		// 1. Look up the terminal ID for this store
		storeUUID, err := uuid.Parse(req.StoreID)
		if err != nil {
			storeUUID = uuid.MustParse("f4100da2-1111-1111-1111-000000000001") // Fallback to main store
		}

		terminalID := integrations.LookupTerminalID(s.DB, storeUUID)
		if terminalID == "" {
			http.Error(w, `{"error":"no_terminal","message":"No Dojo terminal configured for this store. Use standalone mode."}`, http.StatusUnprocessableEntity)
			return
		}

		// 2. Fetch the order total from the database
		var order struct {
			GrossTotal float64 `db:"gross_total"`
		}
		if err := s.DB.Get(&order, `SELECT gross_total FROM orders WHERE id = $1`, orderID); err != nil {
			log.Printf("Dojo Payment: Failed to fetch order %s: %v", orderID, err)
			http.Error(w, `{"error":"order_not_found"}`, http.StatusNotFound)
			return
		}

		// 3. Call the Dojo Cloud API to wake the physical terminal
		dojoClient := integrations.NewDojoClient()
		internalOrder := models.InternalOrder{
			ID:         uuid.MustParse(orderID),
			GrossTotal: order.GrossTotal,
		}

		intentID, err := dojoClient.CreatePaymentIntent(internalOrder, terminalID)
		if err != nil {
			log.Printf("Dojo Payment: Failed to create intent for order %s: %v", orderID, err)
			http.Error(w, fmt.Sprintf(`{"error":"dojo_failed","message":"%s"}`, err.Error()), http.StatusBadGateway)
			return
		}

		// 4. Mark order as awaiting payment in PostgreSQL
		updateQuery := `UPDATE orders SET payment_status = 'awaiting_payment', payment_method = 'dojo', dojo_intent_id = $1 WHERE id = $2`
		if _, err := s.DB.Exec(updateQuery, intentID, orderID); err != nil {
			log.Printf("Dojo Payment: Failed to update order %s with intent: %v", orderID, err)
		}

		// 5. Sync Firestore so the POS shows "Awaiting Payment..." badge
		if s.Firebase != nil {
			s.Firebase.UpdateOrderPayment(r.Context(), orderID, false, "dojo")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted) // 202 — payment is in progress, not yet confirmed
		json.NewEncoder(w).Encode(map[string]string{
			"status":    "awaiting_payment",
			"order_id":  orderID,
			"intent_id": intentID,
			"method":    "dojo",
			"message":   "Terminal activated. Awaiting customer tap.",
		})
		return
	}

	// ─── MANUAL PAYMENT (Cash / Card standalone) ───
	if s.DB != nil {
		updateQuery := `UPDATE orders SET payment_status = 'paid', payment_method = $1, payment_reference = $2 WHERE id = $3`
		ref := "MANUAL-" + orderID[:8]
		if _, err := s.DB.Exec(updateQuery, req.Method, ref, orderID); err != nil {
			log.Printf("Failed to record payment: %v", err)
			http.Error(w, `{"error":"server_error","message":"Failed to record payment"}`, http.StatusInternalServerError)
			return
		}
	}

	// Update Firestore to show "PAID" badge on KDS
	if s.Firebase != nil {
		s.Firebase.UpdateOrderPayment(r.Context(), orderID, true, req.Method)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "success",
		"order_id": orderID,
		"payment":  "paid",
		"method":   req.Method,
		"mode":     "manual",
	})
}

// HandleGetOrderHistory fetches historical orders from Postgres.
// GET /api/v1/stores/:id/history?date=YYYY-MM-DD
func (s *Server) HandleGetOrderHistory(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Invalid store ID", http.StatusBadRequest)
		return
	}
	storeID := parts[4]

	if s.DB == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]OrderWithItems{})
		return
	}

	dateFilter := r.URL.Query().Get("date")
	
	var orders []OrderListItem
	var err error

	if dateFilter != "" {
		query := `SELECT id, store_id, order_source, net_total, vat_total, service_charge, gross_total, status, created_at, table_number, customer_name, customer_phone, payment_status, payment_method
			FROM orders 
			WHERE store_id = $1 AND DATE(created_at) = $2
			ORDER BY created_at DESC LIMIT 100`
		err = s.DB.Select(&orders, query, storeID, dateFilter)
	} else {
		query := `SELECT id, store_id, order_source, net_total, vat_total, service_charge, gross_total, status, created_at, table_number, customer_name, customer_phone, payment_status, payment_method
			FROM orders 
			WHERE store_id = $1 AND status IN ('completed', 'cancelled', 'refunded')
			ORDER BY created_at DESC LIMIT 100`
		err = s.DB.Select(&orders, query, storeID)
	}

	if err != nil {
		log.Printf("Failed to fetch order history: %v", err)
		http.Error(w, `{"error":"server_error","message":"Failed to fetch history"}`, http.StatusInternalServerError)
		return
	}

	result := make([]OrderWithItems, len(orders))
	for i, order := range orders {
		var items []OrderItemRow
		itemQuery := `SELECT id, order_id, product_id, name, price_paid FROM order_items WHERE order_id = $1`
		if err := s.DB.Select(&items, itemQuery, order.ID); err != nil {
			log.Printf("Failed to fetch items for order %s: %v", order.ID, err)
			items = []OrderItemRow{}
		}
		result[i] = OrderWithItems{
			OrderListItem: order,
			Items:         items,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
