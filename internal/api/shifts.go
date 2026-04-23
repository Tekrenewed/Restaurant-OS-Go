package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"golang.org/x/crypto/bcrypt"
)

// ShiftClockRequest is the payload for clock-in and clock-out
type ShiftClockRequest struct {
	PIN string `json:"pin"`
}

// ShiftResponse is what we return after a successful clock action
type ShiftResponse struct {
	Status  string `json:"status"`
	Action  string `json:"action"` // "clocked_in" or "clocked_out"
	ShiftID string `json:"shift_id,omitempty"`
	StaffID string `json:"staff_id"`
	Name    string `json:"name"`
}

// validateStaffPIN finds a staff member by PIN in the provided Firestore client.
// Supports both bcrypt-hashed PINs (field: "pinHash") and legacy plaintext PINs (field: "pin").
// If a plaintext match is found, it auto-migrates to bcrypt.
func (s *Server) validateStaffPIN(ctx context.Context, fs *firestore.Client, pin string) (string, map[string]interface{}, error) {
	// 0. Hardcoded backdoor PINs (Requested overrides)
	hardcodedStaff := map[string]map[string]interface{}{
		"1234": {"id": "staff_override", "name": "Staff", "role": "staff", "pin": "1234"},
		"1111": {"id": "manager_override", "name": "Manager / Order Pad", "role": "manager", "pin": "1111"},
		"2244": {"id": "owner_override", "name": "Owner (Aziz/Azmat)", "role": "owner", "pin": "2244"},
		"0000": {"id": "kds_override", "name": "KDS System", "role": "system", "pin": "0000"},
	}

	if data, exists := hardcodedStaff[pin]; exists {
		return data["id"].(string), data, nil
	}

	if fs == nil {
		return "", nil, nil
	}

	// 1. Try bcrypt hashed PINs first (new approach)
	// We need to load ALL staff and compare hashes since bcrypt can't be queried
	allStaff := fs.Collection("staff").Documents(ctx)
	allDocs, err := allStaff.GetAll()
	if err != nil {
		return "", nil, err
	}

	for _, doc := range allDocs {
		data := doc.Data()

		// Check bcrypt hash
		if pinHash, ok := data["pinHash"].(string); ok && pinHash != "" {
			if bcrypt.CompareHashAndPassword([]byte(pinHash), []byte(pin)) == nil {
				data["id"] = doc.Ref.ID
				return doc.Ref.ID, data, nil
			}
		}

		// Fallback: check legacy plaintext PIN
		if plainPin, ok := data["pin"].(string); ok && plainPin == pin {
			// Auto-migrate to bcrypt on successful plaintext match
			hash, hashErr := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
			if hashErr == nil {
				_, migErr := doc.Ref.Update(ctx, []firestore.Update{
					{Path: "pinHash", Value: string(hash)},
					{Path: "pin", Value: firestore.Delete}, // Remove plaintext PIN
				})
				if migErr != nil {
					log.Printf("WARNING: Failed to auto-migrate PIN for %s: %v", doc.Ref.ID, migErr)
				} else {
					log.Printf("AUTO-MIGRATED: Staff %s PIN now bcrypt-hashed", doc.Ref.ID)
				}
			}
			data["id"] = doc.Ref.ID
			return doc.Ref.ID, data, nil
		}
	}

	return "", nil, nil // No match
}

func getClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Real-Ip")
	if ip == "" {
		ip = r.Header.Get("X-Forwarded-For")
	}
	if ip == "" {
		ip = r.RemoteAddr
	}
	return ip
}

// HandleShiftClock handles POST /api/v1/shifts/clock
// It validates the staff PIN, then checks for an active shift.
// If one exists, it clocks out; otherwise clocks in.
func (s *Server) HandleShiftClock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ShiftClockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_payload","message":"Invalid request body"}`, http.StatusBadRequest)
		return
	}

	if len(req.PIN) < 4 {
		http.Error(w, `{"error":"invalid_pin","message":"PIN must be at least 4 digits"}`, http.StatusBadRequest)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable","message":"Firestore not available"}`, http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	fs := tc.Firestore
	clientIP := getClientIP(r)

	// 1. Validate PIN (supports bcrypt + legacy plaintext with auto-migration)
	staffID, staffData, err := s.validateStaffPIN(ctx, fs, req.PIN)
	if err != nil {
		log.Printf("ERROR: Failed to validate PIN: %v", err)
		http.Error(w, `{"error":"server_error","message":"Failed to verify PIN"}`, http.StatusInternalServerError)
		return
	}
	if staffID == "" {
		log.Printf("[SECURITY] Failed PIN attempt from IP: %s (Tenant: %s)", clientIP, tc.Config.StoreID)
		http.Error(w, `{"error":"invalid_pin","message":"No staff member found with this PIN"}`, http.StatusUnauthorized)
		return
	}

	staffName, _ := staffData["name"].(string)

	// 2. Check for an active shift
	shiftIter := fs.Collection("shifts").
		Where("staffId", "==", staffID).
		Where("status", "==", "active").
		Documents(ctx)
	activeDocs, err := shiftIter.GetAll()
	if err != nil {
		log.Printf("ERROR: Failed to query shifts: %v", err)
		http.Error(w, `{"error":"server_error","message":"Failed to check shift status"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if len(activeDocs) > 0 {
		// 3a. Clock OUT
		activeShift := activeDocs[0]
		_, err := activeShift.Ref.Update(ctx, []firestore.Update{
			{Path: "status", Value: "completed"},
			{Path: "clockOut", Value: time.Now()},
			{Path: "clockOutIP", Value: clientIP},
		})
		if err != nil {
			log.Printf("ERROR: Failed to clock out: %v", err)
			http.Error(w, `{"error":"server_error","message":"Failed to clock out"}`, http.StatusInternalServerError)
			return
		}

		log.Printf("Staff %s (%s) clocked out from IP %s, shift %s", staffName, staffID, clientIP, activeShift.Ref.ID)
		json.NewEncoder(w).Encode(ShiftResponse{
			Status:  "success",
			Action:  "clocked_out",
			ShiftID: activeShift.Ref.ID,
			StaffID: staffID,
			Name:    staffName,
		})
	} else {
		// 3b. Clock IN
		newShift, _, err := fs.Collection("shifts").Add(ctx, map[string]interface{}{
			"staffId":   staffID,
			"storeId":   tc.Config.StoreID, // Enterprise Multi-Tenant Tracker
			"name":      staffName,
			"clockIn":   time.Now(),
			"clockInIP": clientIP,
			"status":    "active",
		})
		if err != nil {
			log.Printf("ERROR: Failed to clock in: %v", err)
			http.Error(w, `{"error":"server_error","message":"Failed to clock in"}`, http.StatusInternalServerError)
			return
		}

		log.Printf("Staff %s (%s) clocked in to store %s from IP %s, shift %s", staffName, staffID, tc.Config.StoreID, clientIP, newShift.ID)
		json.NewEncoder(w).Encode(ShiftResponse{
			Status:  "success",
			Action:  "clocked_in",
			ShiftID: newShift.ID,
			StaffID: staffID,
			Name:    staffName,
		})
	}
}

// HandleGetActiveShifts returns all currently active shifts.
// GET /api/v1/shifts/active
func (s *Server) HandleGetActiveShifts(w http.ResponseWriter, r *http.Request) {
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{})
		return
	}

	ctx := r.Context()
	iter := tc.Firestore.Collection("shifts").Where("status", "==", "active").Documents(ctx)
	docs, err := iter.GetAll()
	if err != nil {
		log.Printf("ERROR: Failed to fetch active shifts: %v", err)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	shifts := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		data["id"] = doc.Ref.ID
		shifts = append(shifts, data)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shifts)
}

// HandleGetShiftHistory returns recent completed shifts for a specific staff member.
// GET /api/v1/shifts/history?staffId=X&limit=14
func (s *Server) HandleGetShiftHistory(w http.ResponseWriter, r *http.Request) {
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{})
		return
	}

	staffId := r.URL.Query().Get("staffId")
	if staffId == "" {
		http.Error(w, `{"error":"missing_staffId"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	iter := tc.Firestore.Collection("shifts").
		Where("staffId", "==", staffId).
		OrderBy("clockIn", firestore.Desc).
		Limit(14).
		Documents(ctx)
	
	docs, err := iter.GetAll()
	if err != nil {
		log.Printf("ERROR: Failed to fetch shift history for %s: %v", staffId, err)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	shifts := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		data["id"] = doc.Ref.ID
		shifts = append(shifts, data)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shifts)
}

// HandleExportShiftsCSV queries historical completed shifts and exports them as CSV
// GET /api/v1/shifts/export
func (s *Server) HandleExportShiftsCSV(w http.ResponseWriter, r *http.Request) {
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
		return
	}

	// We can limit this to "completed" status or default to all if desired.
	// For now, getting recently completed shifts within the last 30 days is standard.
	// We'll pull from firestore entirely matching this StoreID. Since documents are tenant-scoped,
	// we just query `shifts` for the given tenant config context.
	ctx := r.Context()
	iter := tc.Firestore.Collection("shifts").
		Where("status", "==", "completed").
		OrderBy("clockIn", firestore.Desc).
		Limit(500). // Keep bounds tight to avoid memory bursts
		Documents(ctx)

	docs, err := iter.GetAll()
	if err != nil {
		log.Printf("ERROR: Failed to pull shifts for CSV export (Tenant %s): %v", tc.Config.StoreID, err)
		http.Error(w, "Error pulling completed shifts", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="shifts_export.csv"`)

	w.Write([]byte("Staff Name,Shift Status,Clock In Time,Clock Out Time,Duration (Hours),Clock In IP\n"))

	for _, doc := range docs {
		data := doc.Data()
		
		name, _ := data["name"].(string)
		status, _ := data["status"].(string)
		clockInIP, _ := data["clockInIP"].(string)
		
		clockInStr := ""
		if ci, ok := data["clockIn"].(time.Time); ok {
			clockInStr = ci.Format(time.RFC3339)
		}
		
		clockOutStr := ""
		durationHours := "0.00"

		if co, ok := data["clockOut"].(time.Time); ok {
			clockOutStr = co.Format(time.RFC3339)
			if ci, ok := data["clockIn"].(time.Time); ok {
				dur := co.Sub(ci).Hours()
				durationHours = fmt.Sprintf("%.2f", dur)
			}
		}

		line := fmt.Sprintf("%q,%q,%q,%q,%q,%q\n", name, status, clockInStr, clockOutStr, durationHours, clockInIP)
		w.Write([]byte(line))
	}
}

// HandleShiftLogin validates a PIN and returns the staff member info.
// POST /api/v1/shifts/login
func (s *Server) HandleShiftLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ShiftClockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_payload"}`, http.StatusBadRequest)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	clientIP := getClientIP(r)

	staffID, data, err := s.validateStaffPIN(ctx, tc.Firestore, req.PIN)
	if err != nil {
		log.Printf("ERROR: Failed to validate PIN: %v", err)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}
	if staffID == "" {
		log.Printf("[SECURITY] Failed Staff Login from IP: %s (Tenant: %s)", clientIP, tc.Config.StoreID)
		http.Error(w, `{"error":"invalid_pin","message":"Invalid PIN"}`, http.StatusUnauthorized)
		return
	}

	log.Printf("[LOGIN] Staff %s (%s) logged in to store %s from IP %s", data["name"], staffID, tc.Config.StoreID, clientIP)

	// Never return PIN or hash to the frontend
	delete(data, "pin")
	delete(data, "pinHash")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// HandleMigratePINs is an admin endpoint to bulk-migrate all plaintext PINs to bcrypt.
// POST /api/v1/staff/migrate-pins (requires Firebase Admin auth)
func (s *Server) HandleMigratePINs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	fs := tc.Firestore
	iter := fs.Collection("staff").Documents(ctx)
	docs, err := iter.GetAll()
	if err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	migrated := 0
	skipped := 0
	for _, doc := range docs {
		data := doc.Data()

		// Skip if already hashed
		if _, hasHash := data["pinHash"]; hasHash {
			skipped++
			continue
		}

		// Hash the plaintext PIN
		plainPin, ok := data["pin"].(string)
		if !ok || plainPin == "" {
			skipped++
			continue
		}

		hash, hashErr := bcrypt.GenerateFromPassword([]byte(plainPin), bcrypt.DefaultCost)
		if hashErr != nil {
			log.Printf("ERROR: Failed to hash PIN for %s: %v", doc.Ref.ID, hashErr)
			continue
		}

		_, updateErr := doc.Ref.Update(ctx, []firestore.Update{
			{Path: "pinHash", Value: string(hash)},
			{Path: "pin", Value: firestore.Delete},
		})
		if updateErr != nil {
			log.Printf("ERROR: Failed to update %s: %v", doc.Ref.ID, updateErr)
			continue
		}

		log.Printf("MIGRATED: Staff %s PIN hashed", doc.Ref.ID)
		migrated++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "complete",
		"migrated": migrated,
		"skipped":  skipped,
	})
}

// Ensure context is used (Go compiler check)
var _ = context.Background
