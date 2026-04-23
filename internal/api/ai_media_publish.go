package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
)

// PublishMediaRequest represents the payload for publishing to a social platform
type PublishMediaRequest struct {
	JobID string `json:"jobId"`
}

// HandlePublishToInstagram simulates publishing the generated media to Instagram
func (s *Server) HandlePublishToInstagram(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PublishMediaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_payload"}`, http.StatusBadRequest)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"server_error","message":"Firestore client not found"}`, http.StatusInternalServerError)
		return
	}

	// 1. Mark as publishing
	_, err := tc.Firestore.Collection("ai_media_library").Doc(req.JobID).Set(r.Context(), map[string]interface{}{
		"status": "published_ig",
		"publishedAt": time.Now().UnixMilli(),
	}, firestore.MergeAll)

	if err != nil {
		log.Printf("Failed to update status: %v", err)
	}

	log.Printf("[SOCIAL PUBLISH] Simulating Instagram Publish for JobID: %s", req.JobID)
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok", "message": "Successfully published to Instagram"}`))
}

// HandlePublishToTikTok simulates publishing the generated media to TikTok
func (s *Server) HandlePublishToTikTok(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PublishMediaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_payload"}`, http.StatusBadRequest)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"server_error","message":"Firestore client not found"}`, http.StatusInternalServerError)
		return
	}

	// 1. Mark as publishing
	_, err := tc.Firestore.Collection("ai_media_library").Doc(req.JobID).Set(r.Context(), map[string]interface{}{
		"status": "published_tt",
		"publishedAt": time.Now().UnixMilli(),
	}, firestore.MergeAll)

	if err != nil {
		log.Printf("Failed to update status: %v", err)
	}

	log.Printf("[SOCIAL PUBLISH] Simulating TikTok Publish for JobID: %s", req.JobID)
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok", "message": "Successfully published to TikTok"}`))
}
