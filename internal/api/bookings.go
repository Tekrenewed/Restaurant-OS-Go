package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

// Booking defines the structure for a restaurant reservation.
type Booking struct {
	ID            string    `json:"id" db:"id"`
	CustomerName  string    `json:"customerName" db:"customer_name"`
	CustomerPhone string    `json:"customerPhone" db:"customer_phone"`
	Email         string    `json:"email" db:"email"`
	Date          string    `json:"date" db:"booking_date"`
	Time          string    `json:"time" db:"booking_time"`
	Guests        int       `json:"guests" db:"guests"`
	Status        string    `json:"status" db:"status"`
	TableID       string    `json:"tableId" db:"table_id"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

// HandleCreateBooking performs a dual-write (Postgres + Firestore) for a new booking.
func (s *Server) HandleCreateBooking(w http.ResponseWriter, r *http.Request) {
	var b Booking
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if b.ID == "" {
		b.ID = fmt.Sprintf("bk_%d", time.Now().UnixNano())
	}
	if b.Status == "" {
		b.Status = "PENDING"
	}
	b.Status = strings.ToUpper(b.Status)
	b.CreatedAt = time.Now()

	// 1. Write to Postgres
	if s.DB != nil {
		query := `
			INSERT INTO bookings (id, customer_name, customer_phone, email, booking_date, booking_time, guests, status, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`
		_, err := s.DB.Exec(query, b.ID, b.CustomerName, b.CustomerPhone, b.Email, b.Date, b.Time, b.Guests, b.Status, b.CreatedAt)
		if err != nil {
			log.Printf("[HandleCreateBooking] Postgres insert failed: %v", err)
			// Continue to Firestore fallback even if Postgres fails, for resilience
		}
	}

	// 2. Write to Firestore
	fsClient := s.GetFirestoreForRequest(r)
	if fsClient != nil && fsClient.Firestore != nil {
		_, err := fsClient.Firestore.Collection("bookings").Doc(b.ID).Set(context.Background(), b)
		if err != nil {
			log.Printf("[HandleCreateBooking] Firestore sync failed: %v", err)
			http.Error(w, "Failed to sync booking to Firestore", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(b)
}

// HandleUpdateBooking performs a dual-write (Postgres + Firestore) to update a booking.
func (s *Server) HandleUpdateBooking(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Booking ID required", http.StatusBadRequest)
		return
	}
	bookingID := parts[4] // /api/v1/bookings/{id}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// 1. Update Postgres
	if s.DB != nil && len(updates) > 0 {
		setClauses := []string{}
		args := []interface{}{}
		argID := 1
		for k, v := range updates {
			// Map Firestore JSON keys to Postgres columns
			col := k
			if k == "customerName" {
				col = "customer_name"
			} else if k == "customerPhone" {
				col = "customer_phone"
			} else if k == "date" {
				col = "booking_date"
			} else if k == "time" {
				col = "booking_time"
			} else if k == "status" {
				// Ensure status is uppercase
				if strVal, ok := v.(string); ok {
					v = strings.ToUpper(strVal)
					updates[k] = v // update the map for Firestore
				}
			}
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, argID))
			args = append(args, v)
			argID++
		}
		args = append(args, bookingID)

		query := fmt.Sprintf("UPDATE bookings SET %s WHERE id = $%d", strings.Join(setClauses, ", "), argID)
		_, err := s.DB.Exec(query, args...)
		if err != nil {
			log.Printf("[HandleUpdateBooking] Postgres update failed for %s: %v", bookingID, err)
		}
	}

	// 2. Update Firestore
	fsClient := s.GetFirestoreForRequest(r)
	if fsClient != nil && fsClient.Firestore != nil {
		var fsUpdates []firestore.Update
		for k, v := range updates {
			fsUpdates = append(fsUpdates, firestore.Update{Path: k, Value: v})
		}
		if len(fsUpdates) > 0 {
			_, err := fsClient.Firestore.Collection("bookings").Doc(bookingID).Update(context.Background(), fsUpdates)
			if err != nil {
				log.Printf("[HandleUpdateBooking] Firestore update failed for %s: %v", bookingID, err)
				http.Error(w, "Failed to update Firestore", http.StatusInternalServerError)
				return
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}
