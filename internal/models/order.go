package models

import (
	"time"

	"github.com/google/uuid"
)

// OrderItem represents a single item in an order
type OrderItem struct {
	ID          uuid.UUID `json:"id" db:"id"`
	OrderID     uuid.UUID `json:"order_id" db:"order_id"`
	ProductID   uuid.UUID `json:"product_id" db:"product_id"`
	Name        string    `json:"name" db:"name"`
	PricePaid   float64   `json:"price_paid" db:"price_paid"`
	IsTakeaway  bool      `json:"is_takeaway" db:"is_takeaway"`
	IsZeroRated bool      `json:"is_zero_rated" db:"-"` // Used for calculation but not directly stored
	VATRate     float64   `json:"vat_rate" db:"vat_rate"`
}

// InternalOrder is the standardized format for all 4 branches across POS, Kiosk, and Delivery Apps
type InternalOrder struct {
	ID            uuid.UUID   `json:"id" db:"id"`
	ExternalID    string      `json:"external_id" db:"-"` // Used for UberEats, JustEat, Deliveroo
	StoreID       uuid.UUID   `json:"store_id" db:"store_id"`
	Source        string      `json:"source" db:"order_source"` // e.g. "POS", "UberEats", "Deliveroo", "JustEat", "Web"
	Items         []OrderItem `json:"items" db:"-"`
	NetTotal      float64     `json:"net_total" db:"net_total"`
	VATTotal      float64     `json:"vat_total" db:"vat_total"`
	ServiceCharge float64     `json:"service_charge" db:"service_charge"`
	GrossTotal    float64     `json:"gross_total" db:"gross_total"`
	Status        string      `json:"status" db:"status"`
	CreatedAt     time.Time   `json:"created_at" db:"created_at"`

	// Added for QR Table Ordering + Collection
	TableNumber   *int        `json:"table_number,omitempty" db:"table_number"`
	CustomerName  *string     `json:"customer_name,omitempty" db:"customer_name"`
	CustomerPhone *string     `json:"customer_phone,omitempty" db:"customer_phone"`
	CustomerEmail *string     `json:"customer_email,omitempty" db:"customer_email"`
	CustomerID    *string     `json:"customer_id,omitempty" db:"customer_id"` // FK → customers.id

	ApplyServiceCharge bool `json:"apply_service_charge" db:"-"` // Toggle switch from the UI
}

// OrderSummary is what gets returned to the UI or Kiosk after calculation
type OrderSummary struct {
	OrderID       string  `json:"order_id"`
	NetTotal      float64 `json:"net_total"`
	VATTotal      float64 `json:"vat_total"`
	ServiceCharge float64 `json:"service_charge"`
	GrossTotal    float64 `json:"gross_total"`
}
