package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// SendEmailRequest is the payload for dispatching emails via the Go API.
// This decouples the frontend from directly writing to Firestore's `mail` collection.
type SendEmailRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

// HandleSendEmail queues an email via the Firebase Trigger Email extension.
// POST /api/v1/mail/send
// Architecture: Postgres logs the send → Firestore triggers the actual delivery
func (s *Server) HandleSendEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SendEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_payload"}`, http.StatusBadRequest)
		return
	}

	if req.To == "" || req.Subject == "" || req.HTML == "" {
		http.Error(w, `{"error":"validation","message":"to, subject, and html are required"}`, http.StatusBadRequest)
		return
	}

	// 1. Postgres: log the email dispatch (analytics, campaign tracking)
	if s.DB != nil {
		_, err := s.DB.Exec(`
			INSERT INTO campaigns (name, segment, offer_type, offer_value, channel, message_html, status, sent_at, recipients_count)
			VALUES ($1, 'INDIVIDUAL', 'custom', 0, 'email', $2, 'sent', $3, 1)
		`, req.Subject, req.HTML, time.Now())
		if err != nil {
			log.Printf("WARN: Postgres email log failed: %v (proceeding with dispatch)", err)
		}
	}

	// 2. Firestore: write to `mail` collection — Firebase Trigger Email Extension picks this up
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	_, _, err := tc.Firestore.Collection("mail").Add(ctx, map[string]interface{}{
		"to": req.To,
		"message": map[string]interface{}{
			"subject": req.Subject,
			"html":    req.HTML,
		},
	})
	if err != nil {
		log.Printf("ERROR: Failed to queue email to %s: %v", req.To, err)
		http.Error(w, `{"error":"server_error","message":"Failed to queue email"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("[MAIL] Queued email to %s: %s", req.To, req.Subject)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "queued", "to": req.To})
}
