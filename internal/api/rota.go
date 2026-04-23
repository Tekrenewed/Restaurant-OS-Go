package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

// RotaEntry represents a single scheduled shift for a staff member
type RotaEntry struct {
	ID        string `json:"id"`
	StaffID   string `json:"staff_id"`
	StaffName string `json:"staff_name"`
	Day       string `json:"day"`        // ISO date: "2026-04-14"
	StartTime string `json:"start_time"` // "09:00"
	EndTime   string `json:"end_time"`   // "17:00"
	Notes     string `json:"notes,omitempty"`
}

const ROTA_COLLECTION = "rota"

// HandleGetRota returns scheduled shifts for a given week.
// GET /api/v1/rota?week=2026-W16  OR  ?from=2026-04-14&to=2026-04-20
func (s *Server) HandleGetRota(w http.ResponseWriter, r *http.Request) {
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]RotaEntry{})
		return
	}

	ctx := r.Context()
	fs := tc.Firestore

	// Parse date range
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	if fromStr == "" || toStr == "" {
		// Default to current week (Mon-Sun)
		now := time.Now()
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		monday := now.AddDate(0, 0, -(weekday - 1))
		sunday := monday.AddDate(0, 0, 6)
		fromStr = monday.Format("2006-01-02")
		toStr = sunday.Format("2006-01-02")
	}

	// Query Firestore for rota entries in date range
	iter := fs.Collection(ROTA_COLLECTION).
		Where("day", ">=", fromStr).
		Where("day", "<=", toStr).
		OrderBy("day", firestore.Asc).
		Documents(ctx)

	docs, err := iter.GetAll()
	if err != nil {
		log.Printf("Rota: Failed to fetch schedule: %v", err)
		http.Error(w, `{"error":"server_error","message":"Failed to fetch schedule"}`, http.StatusInternalServerError)
		return
	}

	entries := make([]RotaEntry, 0, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		entry := RotaEntry{
			ID: doc.Ref.ID,
		}
		if v, ok := data["staff_id"].(string); ok {
			entry.StaffID = v
		}
		if v, ok := data["staff_name"].(string); ok {
			entry.StaffName = v
		}
		if v, ok := data["day"].(string); ok {
			entry.Day = v
		}
		if v, ok := data["start_time"].(string); ok {
			entry.StartTime = v
		}
		if v, ok := data["end_time"].(string); ok {
			entry.EndTime = v
		}
		if v, ok := data["notes"].(string); ok {
			entry.Notes = v
		}
		entries = append(entries, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// HandleSetRota creates or updates a shift in the rota.
// POST /api/v1/rota
// Body: { "staff_id": "...", "staff_name": "Ahmed", "day": "2026-04-14", "start_time": "09:00", "end_time": "17:00", "notes": "" }
// If "id" is provided, updates the existing entry. Otherwise creates a new one.
func (s *Server) HandleSetRota(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	var entry RotaEntry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		http.Error(w, `{"error":"invalid_payload"}`, http.StatusBadRequest)
		return
	}

	// Validate required fields
	if entry.StaffID == "" || entry.Day == "" || entry.StartTime == "" || entry.EndTime == "" {
		http.Error(w, `{"error":"missing_fields","message":"staff_id, day, start_time, end_time are required"}`, http.StatusBadRequest)
		return
	}

	// Validate date format
	if _, err := time.Parse("2006-01-02", entry.Day); err != nil {
		http.Error(w, `{"error":"invalid_date","message":"day must be YYYY-MM-DD"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	fs := tc.Firestore

	data := map[string]interface{}{
		"staff_id":   entry.StaffID,
		"staff_name": entry.StaffName,
		"day":        entry.Day,
		"start_time": entry.StartTime,
		"end_time":   entry.EndTime,
		"notes":      entry.Notes,
		"updated_at": time.Now(),
	}

	if entry.ID != "" {
		// Update existing
		_, err := fs.Collection(ROTA_COLLECTION).Doc(entry.ID).Set(ctx, data)
		if err != nil {
			log.Printf("Rota: Failed to update entry: %v", err)
			http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
			return
		}
	} else {
		// Create new
		ref, _, err := fs.Collection(ROTA_COLLECTION).Add(ctx, data)
		if err != nil {
			log.Printf("Rota: Failed to create entry: %v", err)
			http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
			return
		}
		entry.ID = ref.ID
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"id":     entry.ID,
	})
}

// HandleDeleteRota removes a scheduled shift.
// DELETE /api/v1/rota/:id
func (s *Server) HandleDeleteRota(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	// Extract rota ID from path: /api/v1/rota/{id}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	rotaID := parts[4]

	ctx := r.Context()
	_, err := tc.Firestore.Collection(ROTA_COLLECTION).Doc(rotaID).Delete(ctx)
	if err != nil {
		log.Printf("Rota: Failed to delete entry %s: %v", rotaID, err)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
		"id":     rotaID,
	})
}

// HandleGetStaffNextShift returns the next scheduled shift for a staff member.
// GET /api/v1/rota/next?staff_id=xxx
// Used by the PinPad to show "Your next shift: Tomorrow 9:00 - 17:00"
func (s *Server) HandleGetStaffNextShift(w http.ResponseWriter, r *http.Request) {
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "no_schedule"})
		return
	}

	staffID := r.URL.Query().Get("staff_id")
	if staffID == "" {
		http.Error(w, `{"error":"missing_staff_id"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	today := time.Now().Format("2006-01-02")

	iter := tc.Firestore.Collection(ROTA_COLLECTION).
		Where("staff_id", "==", staffID).
		Where("day", ">=", today).
		OrderBy("day", firestore.Asc).
		Limit(1).
		Documents(ctx)

	docs, err := iter.GetAll()
	if err != nil || len(docs) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "no_upcoming_shifts"})
		return
	}

	data := docs[0].Data()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "scheduled",
		"id":         docs[0].Ref.ID,
		"day":        data["day"],
		"start_time": data["start_time"],
		"end_time":   data["end_time"],
		"notes":      data["notes"],
	})
}
