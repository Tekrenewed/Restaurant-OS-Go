package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"golang.org/x/crypto/bcrypt"
)

// ─── Staff CRUD ───────────────────────────────────────────────────────────────
// Architecture: Postgres = source of truth → Firestore = real-time cache
// All writes go to Postgres first, then sync to Firestore for live UI updates.

type CreateStaffRequest struct {
	Name string `json:"name"`
	PIN  string `json:"pin"`
	Role string `json:"role"`
}

// HandleListStaff returns all staff members (excluding PINs).
// GET /api/v1/staff
// Source: Postgres (source of truth), falls back to Firestore if DB unavailable
func (s *Server) HandleListStaff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Try Postgres first (source of truth)
	if s.DB != nil {
		var staff []struct {
			ID     string  `json:"id" db:"id"`
			Name   string  `json:"name" db:"name"`
			Role   *string `json:"role" db:"role"`
			Points int     `json:"points" db:"points"`
			Active bool    `json:"is_active" db:"is_active"`
		}
		err := s.DB.Select(&staff, `
			SELECT id, name, role, COALESCE(points, 0) as points, is_active
			FROM staff WHERE is_active = true ORDER BY name ASC
		`)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(staff)
			return
		}
		log.Printf("WARN: Postgres staff query failed, falling back to Firestore: %v", err)
	}

	// Fallback: Firestore (cache layer)
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{})
		return
	}

	ctx := r.Context()
	iter := tc.Firestore.Collection("staff").Documents(ctx)
	docs, err := iter.GetAll()
	if err != nil {
		log.Printf("ERROR: Failed to list staff: %v", err)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	result := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		data["id"] = doc.Ref.ID
		delete(data, "pin")
		delete(data, "pinHash")
		result = append(result, data)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// HandleCreateStaff creates a new staff member with a bcrypt-hashed PIN.
// POST /api/v1/staff
// Flow: Postgres INSERT → Firestore sync
func (s *Server) HandleCreateStaff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateStaffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_payload"}`, http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Name) == "" || len(req.PIN) != 4 {
		http.Error(w, `{"error":"validation","message":"Name is required and PIN must be 4 digits"}`, http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = "staff"
	}

	// Hash the PIN
	hash, hashErr := bcrypt.GenerateFromPassword([]byte(req.PIN), bcrypt.DefaultCost)
	if hashErr != nil {
		log.Printf("ERROR: Failed to hash PIN: %v", hashErr)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	name := strings.TrimSpace(req.Name)
	var staffID string

	// 1. Postgres INSERT (source of truth)
	if s.DB != nil {
		// Check duplicate PIN in Postgres
		var existingCount int
		_ = s.DB.Get(&existingCount, `SELECT COUNT(*) FROM staff WHERE pin = $1 AND is_active = true`, req.PIN)
		if existingCount > 0 {
			http.Error(w, `{"error":"duplicate_pin","message":"This PIN is already taken. Choose a different one."}`, http.StatusConflict)
			return
		}

		err := s.DB.QueryRow(`
			INSERT INTO staff (name, pin, pin_hash, role, points, is_active)
			VALUES ($1, $2, $3, $4, 0, true)
			RETURNING id
		`, name, req.PIN, string(hash), req.Role).Scan(&staffID)
		if err != nil {
			log.Printf("ERROR: Failed to insert staff into Postgres: %v", err)
			http.Error(w, `{"error":"server_error","message":"Failed to create staff member"}`, http.StatusInternalServerError)
			return
		}
		log.Printf("[STAFF] Created in Postgres: %s (%s) role=%s", name, staffID, req.Role)
	}

	// 2. Firestore sync (real-time cache for frontend subscriptions)
	tc := s.GetFirestoreForRequest(r)
	if tc != nil && tc.Firestore != nil {
		ctx := r.Context()
		fs := tc.Firestore

		// If Postgres gave us an ID, use it; otherwise let Firestore auto-generate
		firestoreData := map[string]interface{}{
			"name":      name,
			"pinHash":   string(hash),
			"role":      req.Role,
			"points":    0,
			"createdAt": time.Now(),
		}

		if staffID != "" {
			// Use Postgres UUID as the Firestore doc ID for consistency
			_, err := fs.Collection("staff").Doc(staffID).Set(ctx, firestoreData)
			if err != nil {
				log.Printf("WARN: Firestore staff sync failed for %s: %v", staffID, err)
			}
		} else {
			// No Postgres — Firestore-only mode (fallback)
			// Also need to check duplicate PINs in Firestore
			allStaff := fs.Collection("staff").Documents(ctx)
			allDocs, _ := allStaff.GetAll()
			for _, doc := range allDocs {
				data := doc.Data()
				if pinHash, ok := data["pinHash"].(string); ok && pinHash != "" {
					if bcrypt.CompareHashAndPassword([]byte(pinHash), []byte(req.PIN)) == nil {
						http.Error(w, `{"error":"duplicate_pin","message":"This PIN is already taken. Choose a different one."}`, http.StatusConflict)
						return
					}
				}
				if plainPin, ok := data["pin"].(string); ok && plainPin == req.PIN {
					http.Error(w, `{"error":"duplicate_pin","message":"This PIN is already taken. Choose a different one."}`, http.StatusConflict)
					return
				}
			}
			ref, _, err := fs.Collection("staff").Add(ctx, firestoreData)
			if err != nil {
				log.Printf("ERROR: Failed to create staff in Firestore: %v", err)
				http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
				return
			}
			staffID = ref.ID
		}
	}

	if staffID == "" {
		http.Error(w, `{"error":"service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	log.Printf("[STAFF] Created: %s (%s) role=%s", name, staffID, req.Role)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":   staffID,
		"name": name,
		"role": req.Role,
	})
}

// HandleDeleteStaff soft-deletes a staff member by ID.
// DELETE /api/v1/staff/{id}
// Flow: Postgres UPDATE is_active=false → Firestore delete doc
func (s *Server) HandleDeleteStaff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	staffID := parts[len(parts)-1]
	if staffID == "" || staffID == "staff" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}

	// 1. Postgres soft-delete (source of truth)
	if s.DB != nil {
		_, err := s.DB.Exec(`UPDATE staff SET is_active = false WHERE id = $1`, staffID)
		if err != nil {
			log.Printf("WARN: Postgres staff soft-delete failed for %s: %v", staffID, err)
		} else {
			log.Printf("[STAFF] Soft-deleted in Postgres: %s", staffID)
		}
	}

	// 2. Firestore hard-delete (cache — no need to keep stale docs)
	tc := s.GetFirestoreForRequest(r)
	if tc != nil && tc.Firestore != nil {
		ctx := r.Context()
		_, err := tc.Firestore.Collection("staff").Doc(staffID).Delete(ctx)
		if err != nil {
			log.Printf("WARN: Firestore staff delete failed for %s: %v", staffID, err)
		}
	}

	log.Printf("[STAFF] Deleted member: %s", staffID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": staffID})
}

// HandleIncrementStaffPoints adds gamification points to a staff member.
// POST /api/v1/staff/{id}/points
// Flow: Postgres UPDATE → Firestore Increment (both get the points)
func (s *Server) HandleIncrementStaffPoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	staffID := ""
	for i, part := range parts {
		if part == "staff" && i+1 < len(parts) && parts[i+1] != "points" {
			staffID = parts[i+1]
			break
		}
	}
	if staffID == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		Amount int `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount == 0 {
		req.Amount = 1
	}

	// 1. Postgres UPDATE (source of truth)
	if s.DB != nil {
		_, err := s.DB.Exec(`UPDATE staff SET points = COALESCE(points, 0) + $1 WHERE id = $2`, req.Amount, staffID)
		if err != nil {
			log.Printf("WARN: Postgres points update failed for %s: %v", staffID, err)
		}
	}

	// 2. Firestore Increment (real-time cache for frontend)
	tc := s.GetFirestoreForRequest(r)
	if tc != nil && tc.Firestore != nil {
		ctx := r.Context()
		_, err := tc.Firestore.Collection("staff").Doc(staffID).Update(ctx, []firestore.Update{
			{Path: "points", Value: firestore.Increment(req.Amount)},
		})
		if err != nil {
			log.Printf("WARN: Firestore points increment failed for %s: %v", staffID, err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "added": req.Amount})
}
