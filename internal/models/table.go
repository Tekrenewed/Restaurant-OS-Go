package models

import (
	"time"

	"github.com/google/uuid"
)

// TableStatus represents the current state of a dine-in table
type TableStatus string

const (
	TableStatusAvailable TableStatus = "AVAILABLE"
	TableStatusOccupied  TableStatus = "OCCUPIED"
	TableStatusReserved  TableStatus = "RESERVED"
	TableStatusDirty     TableStatus = "DIRTY"
)

// Table represents a physical dine-in table in the restaurant
type Table struct {
	ID          uuid.UUID   `json:"id" db:"id"`
	StoreID     uuid.UUID   `json:"store_id" db:"store_id"`
	TableNumber int         `json:"table_number" db:"table_number"`
	Capacity    int         `json:"capacity" db:"capacity"`
	Section     string      `json:"section" db:"section"` // e.g. "Main Floor", "VIP", "Patio"
	Status      TableStatus `json:"status" db:"status"`
	CurrentOrderID *uuid.UUID `json:"current_order_id" db:"current_order_id"`
}

// ReservationStatus represents the state of a booking
type ReservationStatus string

const (
	ReservationStatusPending   ReservationStatus = "PENDING"
	ReservationStatusConfirmed ReservationStatus = "CONFIRMED"
	ReservationStatusSeated    ReservationStatus = "SEATED"
	ReservationStatusCompleted ReservationStatus = "COMPLETED"
	ReservationStatusCancelled ReservationStatus = "CANCELLED"
	ReservationStatusNoShow    ReservationStatus = "NO_SHOW"
)

// Reservation represents a booking for a table at a specific time
type Reservation struct {
	ID              uuid.UUID         `json:"id" db:"id"`
	StoreID         uuid.UUID         `json:"store_id" db:"store_id"`
	TableID         *uuid.UUID        `json:"table_id" db:"table_id"` // Can be unassigned initially
	CustomerName    string            `json:"customer_name" db:"customer_name"`
	CustomerPhone   string            `json:"customer_phone" db:"customer_phone"`
	CustomerEmail   string            `json:"customer_email" db:"customer_email"`
	PartySize       int               `json:"party_size" db:"party_size"`
	ReservationTime time.Time         `json:"reservation_time" db:"reservation_time"`
	DurationMinutes int               `json:"duration_minutes" db:"duration_minutes"` // e.g. 90 minutes
	Status          ReservationStatus `json:"status" db:"status"`
	SpecialRequests string            `json:"special_requests" db:"special_requests"`
	CreatedAt       time.Time         `json:"created_at" db:"created_at"`
}
