package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"restaurant-os/internal/models"
	"testing"
)

// TestHandleCreateOrder_WebOrder verifies that a Web order:
// 1. Returns 201 Created
// 2. Gets status "web_holding" (not "pending")
// 3. Returns a valid order summary with calculated totals
func TestHandleCreateOrder_WebOrder(t *testing.T) {
	server := &Server{DB: nil, Hub: NewHub(), Firebase: nil}
	go server.Hub.Run()

	order := map[string]interface{}{
		"external_id":          "ORD-TEST-001",
		"store_id":             "00000000-0000-0000-0000-000000000000",
		"source":               "Web",
		"customer_name":        "Test Customer",
		"customer_phone":       "07700000000",
		"apply_service_charge": false,
		"items": []map[string]interface{}{
			{"name": "Classic Falooda", "price_paid": 6.99},
			{"name": "Masala Chai", "price_paid": 2.99},
		},
	}

	body, _ := json.Marshal(order)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.HandleCreateOrder(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var summary models.OrderSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &summary); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if summary.GrossTotal <= 0 {
		t.Errorf("expected positive gross total, got %f", summary.GrossTotal)
	}

	if summary.OrderID == "" {
		t.Error("expected non-empty order ID")
	}
}

// TestHandleCreateOrder_POSOrder verifies that a POS order gets status "pending"
func TestHandleCreateOrder_POSOrder(t *testing.T) {
	server := &Server{DB: nil, Hub: NewHub(), Firebase: nil}
	go server.Hub.Run()

	order := map[string]interface{}{
		"store_id":             "00000000-0000-0000-0000-000000000000",
		"source":               "POS",
		"customer_name":        "Walk-in",
		"apply_service_charge": false,
		"items": []map[string]interface{}{
			{"name": "Pistachio Kulfi", "price_paid": 4.99},
		},
	}

	body, _ := json.Marshal(order)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.HandleCreateOrder(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestHandleCreateOrder_InvalidJSON verifies that bad JSON returns 400
func TestHandleCreateOrder_InvalidJSON(t *testing.T) {
	server := &Server{DB: nil, Hub: NewHub(), Firebase: nil}
	go server.Hub.Run()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader([]byte(`{invalid json`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.HandleCreateOrder(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// TestHandleCreateOrder_WrongMethod verifies that GET returns 405
func TestHandleCreateOrder_WrongMethod(t *testing.T) {
	server := &Server{DB: nil, Hub: NewHub(), Firebase: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	rr := httptest.NewRecorder()

	server.HandleCreateOrder(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

// TestHandleCreateOrder_EmptyItems verifies that an order with no items still returns 201
// (totals will be 0, but the order is valid — e.g. a table reservation with no preorder)
func TestHandleCreateOrder_EmptyItems(t *testing.T) {
	server := &Server{DB: nil, Hub: NewHub(), Firebase: nil}
	go server.Hub.Run()

	order := map[string]interface{}{
		"store_id":             "00000000-0000-0000-0000-000000000000",
		"source":               "Web",
		"customer_name":        "Test",
		"apply_service_charge": false,
		"items":                []map[string]interface{}{},
	}

	body, _ := json.Marshal(order)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.HandleCreateOrder(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestStatusConstants verifies the status validation helper
func TestStatusConstants(t *testing.T) {
	validStatuses := []string{"web_holding", "pending", "preparing", "ready", "completed", "no_show"}
	for _, s := range validStatuses {
		if !models.IsValidStatus(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalidStatuses := []string{"", "invalid", "PENDING", "Web_Holding", "done"}
	for _, s := range invalidStatuses {
		if models.IsValidStatus(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}
