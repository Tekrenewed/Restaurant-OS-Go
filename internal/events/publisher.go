package events

import "context"

// ─── RealtimePublisher ───
// The "standard plug socket" interface.
// Today: FirestorePublisher implements this.
// Future: RedisPublisher will implement this with zero changes to handlers.
//
// Every handler calls publisher.PublishXxx() instead of writing directly to Firestore.
// The publisher decides HOW and WHERE to broadcast — the handler doesn't care.
type RealtimePublisher interface {
	// Orders
	PublishOrderCreated(ctx context.Context, order OrderEvent) error
	PublishOrderUpdated(ctx context.Context, orderID string, fields map[string]interface{}) error

	// Bookings
	PublishBookingCreated(ctx context.Context, booking BookingEvent) error
	PublishBookingUpdated(ctx context.Context, bookingID string, fields map[string]interface{}) error

	// Menu
	PublishMenuItemUpdated(ctx context.Context, itemID string, fields map[string]interface{}) error
	PublishSoldOutChanged(ctx context.Context, soldOutItems []string) error
}

// ─── Event Payloads ───
// These are the canonical shapes that flow through the event bus.
// They are intentionally decoupled from both the Postgres model (models.InternalOrder)
// and the Firestore document shape — the adapter translates between them.

// OrderEvent represents an order flowing through the event bus
type OrderEvent struct {
	ID            string              `json:"id"`
	ExternalID    string              `json:"external_id,omitempty"` // ORD-xxx for web orders
	StoreID       string              `json:"store_id"`
	TenantID      string              `json:"tenant_id"`
	CustomerName  string              `json:"customer_name"`
	CustomerPhone string              `json:"customer_phone,omitempty"`
	OrderType     string              `json:"order_type"` // collection, dine-in, delivery, takeaway
	TableNumber   int                 `json:"table_number,omitempty"`
	Items         []OrderItemEvent    `json:"items"`
	Total         float64             `json:"total"`
	Status        string              `json:"status"`
	Source        string              `json:"source"` // POS, Web, Kiosk
	Timestamp     interface{}         `json:"timestamp"`
	Extra         map[string]interface{} `json:"extra,omitempty"` // Extensible metadata
}

// OrderItemEvent represents an item within an order event
type OrderItemEvent struct {
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Quantity int     `json:"quantity"`
	Image    string  `json:"image,omitempty"`
}

// BookingEvent represents a booking flowing through the event bus
type BookingEvent struct {
	ID            string `json:"id"`
	CustomerName  string `json:"customer_name"`
	CustomerPhone string `json:"customer_phone"`
	Email         string `json:"email,omitempty"`
	Date          string `json:"date"`
	Time          string `json:"time"`
	Guests        int    `json:"guests"`
	Status        string `json:"status"`
}
