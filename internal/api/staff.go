package api

import (
	"encoding/json"
	"net/http"

	"github.com/jmoiron/sqlx"
)

type Staff struct {
	ID         string  `json:"id" db:"id"`
	StoreID    string  `json:"store_id" db:"store_id"`
	Name       string  `json:"name" db:"name"`
	Role       string  `json:"role" db:"role"`
	Pin        string  `json:"pin" db:"pin"`
	HourlyRate float64 `json:"hourly_rate" db:"hourly_rate"`
	IsActive   bool    `json:"is_active" db:"is_active"`
}

// HandleClockIn handles staff clock-in via PIN
func HandleClockIn(db *sqlx.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Pin     string `json:"pin"`
			StoreID string `json:"store_id"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Find staff by PIN
		var staff Staff
		err := db.Get(&staff, `SELECT * FROM staff WHERE pin = $1 AND store_id = $2 AND is_active = true`, req.Pin, req.StoreID)
		if err != nil {
			http.Error(w, "Invalid PIN", http.StatusUnauthorized)
			return
		}

		// Create shift
		_, err = db.Exec(`
			INSERT INTO shifts (staff_id, store_id, start_time, status)
			VALUES ($1, $2, NOW(), 'active')
		`, staff.ID, req.StoreID)

		if err != nil {
			http.Error(w, "Failed to start shift", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "Clocked in successfully", "staff_name": staff.Name})
	}
}
