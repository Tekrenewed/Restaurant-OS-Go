package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// HandleSaveSettings writes a settings document.
// POST /api/v1/settings/{settingId}
// Architecture: Firestore is the canonical store for settings (real-time config).
// No Postgres table for settings — they're runtime config, not relational data.
func (s *Server) HandleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract setting ID from URL path: /api/v1/settings/{id}
	parts := splitPath(r.URL.Path)
	if len(parts) < 4 {
		http.Error(w, `{"error":"missing_setting_id"}`, http.StatusBadRequest)
		return
	}
	settingID := parts[3]

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid_payload"}`, http.StatusBadRequest)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	payload["updatedAt"] = time.Now()

	_, err := tc.Firestore.Collection("settings").Doc(settingID).Set(ctx, payload)
	if err != nil {
		log.Printf("ERROR: Failed to save setting %s: %v", settingID, err)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("[SETTINGS] Saved %s for tenant %s", settingID, tc.Config.StoreID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "saved", "id": settingID})
}

// splitPath helper
func splitPath(path string) []string {
	result := make([]string, 0)
	current := ""
	for _, c := range path {
		if c == '/' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
