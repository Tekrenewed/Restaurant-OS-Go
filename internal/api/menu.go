package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"cloud.google.com/go/firestore"
)

// HandleGet86Board returns the currently sold-out items from the settings/menuConfig document.
// GET /api/v1/menu/sold-out
func (s *Server) HandleGet86Board(w http.ResponseWriter, r *http.Request) {
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]string{})
		return
	}

	ctx := r.Context()
	doc, err := tc.Firestore.Collection("settings").Doc("menuConfig").Get(ctx)
	if err != nil {
		// Document might not exist yet, treat as empty 86 board
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]string{})
		return
	}

	data := doc.Data()
	soldOutItems := make([]string, 0)
	if items, ok := data["soldOutItems"].([]interface{}); ok {
		for _, item := range items {
			if strItem, isStr := item.(string); isStr {
				soldOutItems = append(soldOutItems, strItem)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(soldOutItems)
}

type Toggle86Request struct {
	ItemID string `json:"itemId"`
	Is86d  bool   `json:"is86d"` // True if we are 86ing it, false if we are restoring it
}

// HandleToggle86Board updates the 86 status of a menu item.
// POST /api/v1/menu/sold-out
func (s *Server) HandleToggle86Board(w http.ResponseWriter, r *http.Request) {
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	var req Toggle86Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_payload"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	docRef := tc.Firestore.Collection("settings").Doc("menuConfig")
	
	err := tc.Firestore.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		soldOutItems := make([]string, 0)
		
		if err == nil && doc.Exists() {
			data := doc.Data()
			if items, ok := data["soldOutItems"].([]interface{}); ok {
				for _, item := range items {
					if strItem, isStr := item.(string); isStr {
						soldOutItems = append(soldOutItems, strItem)
					}
				}
			}
		}
		
		// Rebuild the array
		newSoldOuts := make([]string, 0)
		found := false
		for _, id := range soldOutItems {
			if id == req.ItemID {
				found = true
				if req.Is86d {
					// We're 86ing it, keep it
					newSoldOuts = append(newSoldOuts, id)
				}
				// If restoring, we just don't add it to newSoldOuts
			} else {
				newSoldOuts = append(newSoldOuts, id)
			}
		}
		
		if req.Is86d && !found {
			// Item wasn't there but we want to 86 it
			newSoldOuts = append(newSoldOuts, req.ItemID)
		}
		
		return tx.Set(docRef, map[string]interface{}{
			"soldOutItems": newSoldOuts,
		})
	})

	if err != nil {
		log.Printf("ERROR: Failed to toggle 86 item %s: %v", req.ItemID, err)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
