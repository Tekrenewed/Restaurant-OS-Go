package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
)

// AuditEntry represents a single admin action logged for accountability.
type AuditEntry struct {
	Action    string      `json:"action"`    // e.g., "order.status.update", "shift.clock_in", "staff.pin_migrate"
	Actor     string      `json:"actor"`     // UID or staff name
	Target    string      `json:"target"`    // Resource ID (order ID, staff ID, etc.)
	Details   interface{} `json:"details"`   // Arbitrary payload
	IP        string      `json:"ip"`        // Client IP
	Timestamp time.Time   `json:"timestamp"`
}

// LogAuditEvent writes an audit entry to the Firestore 'audit_log' collection.
// This creates a tamper-resistant trail of all admin actions.
func (s *Server) LogAuditEvent(r *http.Request, action, actor, target string, details interface{}) {
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		log.Printf("AUDIT (no Firestore): %s | actor=%s target=%s", action, actor, target)
		return
	}

	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	// Take first IP from chain
	for i, c := range ip {
		if c == ',' {
			ip = ip[:i]
			break
		}
	}

	entry := map[string]interface{}{
		"action":    action,
		"actor":     actor,
		"target":    target,
		"details":   details,
		"ip":        ip,
		"timestamp": time.Now(),
	}

	ctx := r.Context()
	_, _, err := tc.Firestore.Collection("audit_log").Add(ctx, entry)
	if err != nil {
		log.Printf("AUDIT ERROR: Failed to write audit log: %v (action=%s actor=%s target=%s)", err, action, actor, target)
	} else {
		log.Printf("AUDIT: %s | actor=%s target=%s ip=%s", action, actor, target, ip)
	}
}

// HandleGetAuditLog returns recent audit entries. GET /api/v1/audit
// Admin-only — protected by AuthMiddleware in main.go
func (s *Server) HandleGetAuditLog(w http.ResponseWriter, r *http.Request) {
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}

	ctx := r.Context()
	// Return last 100 audit entries, newest first
	iter := tc.Firestore.Collection("audit_log").
		OrderBy("timestamp", firestore.Desc).
		Limit(100).
		Documents(ctx)
	docs, err := iter.GetAll()
	if err != nil {
		log.Printf("ERROR: Failed to fetch audit log: %v", err)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	entries := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		data["id"] = doc.Ref.ID
		entries = append(entries, data)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}
