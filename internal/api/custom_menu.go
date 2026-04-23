package api

import (
	"encoding/json"
	"log"
	"net/http"
)

// HandleListCustomMenuItems returns all custom (admin-added) menu items.
// GET /api/v1/menu/custom
// Architecture: Firestore is the source for custom menu items today
// (they're managed via the admin UI and consumed by the frontend).
// Future: migrate to Postgres `products` table with is_custom=true flag.
func (s *Server) HandleListCustomMenuItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Firestore: read from custom_menu_items collection
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{})
		return
	}

	ctx := r.Context()
	iter := tc.Firestore.Collection("custom_menu_items").Documents(ctx)
	docs, err := iter.GetAll()
	if err != nil {
		log.Printf("ERROR: Failed to list custom menu items: %v", err)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	items := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		data["id"] = doc.Ref.ID
		data["isCustom"] = true
		items = append(items, data)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}
