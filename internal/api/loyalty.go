package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"restaurant-os/internal/email"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

const CUSTOMERS_COLLECTION = "customers"

// CustomerRewardResponse is what the POS frontend receives
type CustomerRewardResponse struct {
	Phone         string                   `json:"phone"`
	Name          string                   `json:"name"`
	TotalOrders   int                      `json:"total_orders"`
	TotalSpent    float64                  `json:"total_spent"`
	Rewards       []map[string]interface{} `json:"rewards"`
	NextMilestone string                   `json:"next_milestone,omitempty"`
}

// HandleGetCustomerRewards returns available loyalty rewards for a customer.
// GET /api/v1/loyalty/:phone
func (s *Server) HandleGetCustomerRewards(w http.ResponseWriter, r *http.Request) {
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	// Extract phone from path: /api/v1/loyalty/{phone}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, `{"error":"missing_phone"}`, http.StatusBadRequest)
		return
	}
	phone := normalisePhone(parts[4])

	ctx := r.Context()
	docRef := tc.Firestore.Collection(CUSTOMERS_COLLECTION).Doc(phone)
	snap, err := docRef.Get(ctx)
	if err != nil {
		// Customer not found — return empty profile
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CustomerRewardResponse{
			Phone:   phone,
			Rewards: []map[string]interface{}{},
		})
		return
	}

	data := snap.Data()
	response := CustomerRewardResponse{
		Phone: phone,
	}

	if v, ok := data["name"].(string); ok {
		response.Name = v
	}
	if v, ok := data["totalOrders"].(int64); ok {
		response.TotalOrders = int(v)
	}
	if v, ok := data["totalSpent"].(float64); ok {
		response.TotalSpent = v
	}

	// Filter to only available, non-expired rewards
	now := time.Now()
	if rewards, ok := data["rewards"].([]interface{}); ok {
		for _, r := range rewards {
			reward, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			status, _ := reward["status"].(string)
			if status != "available" {
				continue
			}
			// Check expiry
			if expiresStr, ok := reward["expiresAt"].(string); ok {
				if expires, err := time.Parse(time.RFC3339, expiresStr); err == nil {
					if expires.Before(now) {
						continue
					}
				}
			}
			response.Rewards = append(response.Rewards, reward)
		}
	}

	if response.Rewards == nil {
		response.Rewards = []map[string]interface{}{}
	}

	// Calculate next milestone hint
	categoryCounts, _ := data["categoryCounts"].(map[string]interface{})
	if categoryCounts != nil {
		faloodaCount := 0
		if v, ok := categoryCounts["falooda"].(int64); ok {
			faloodaCount = int(v)
		}
		remaining := 5 - (faloodaCount % 5)
		if remaining > 0 && remaining < 5 {
			response.NextMilestone = fmt.Sprintf("%d more faloodas until your next free one! 🍨", remaining)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleRedeemReward marks a specific reward as used.
// POST /api/v1/loyalty/:phone/redeem
// Body: { "reward_id": "falooda_5th_free_1234", "order_id": "abc-def" }
func (s *Server) HandleRedeemReward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	// Extract phone: /api/v1/loyalty/{phone}/redeem
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, `{"error":"missing_phone"}`, http.StatusBadRequest)
		return
	}
	phone := normalisePhone(parts[4])

	var req struct {
		RewardID string `json:"reward_id"`
		OrderID  string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_payload"}`, http.StatusBadRequest)
		return
	}

	if req.RewardID == "" {
		http.Error(w, `{"error":"missing_reward_id"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	docRef := tc.Firestore.Collection(CUSTOMERS_COLLECTION).Doc(phone)
	snap, err := docRef.Get(ctx)
	if err != nil {
		http.Error(w, `{"error":"customer_not_found"}`, http.StatusNotFound)
		return
	}

	data := snap.Data()
	rewards, ok := data["rewards"].([]interface{})
	if !ok {
		http.Error(w, `{"error":"no_rewards"}`, http.StatusNotFound)
		return
	}

	// Find and mark the reward as redeemed
	found := false
	now := time.Now()
	for i, r := range rewards {
		reward, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if reward["id"] == req.RewardID && reward["status"] == "available" {
			reward["status"] = "redeemed"
			reward["redeemedAt"] = now.Format(time.RFC3339)
			reward["redeemedOrderId"] = req.OrderID
			rewards[i] = reward
			found = true
			break
		}
	}

	if !found {
		http.Error(w, `{"error":"reward_not_found","message":"Reward not found or already redeemed"}`, http.StatusNotFound)
		return
	}

	// Save updated rewards
	_, err = docRef.Update(ctx, []firestore.Update{
		{Path: "rewards", Value: rewards},
	})
	if err != nil {
		log.Printf("Loyalty: Failed to redeem reward for %s: %v", phone, err)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("Loyalty: Reward %s redeemed for customer %s on order %s", req.RewardID, phone, req.OrderID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "redeemed",
		"reward_id": req.RewardID,
	})
}

// SendLoyaltyRewardEmail sends a notification email when a customer earns a new reward.
// Uses the existing Firestore `mail` collection trigger.
// Now tenant-aware: loads branding from the email_templates table.
func (s *Server) SendLoyaltyRewardEmail(ctx context.Context, fsClient *firestore.Client, storeID string, customerEmail string, customerName string, rewardDescription string) {
	if fsClient == nil || customerEmail == "" {
		return
	}

	// Load tenant branding (falls back to Falooda & Co defaults)
	branding := email.DefaultBranding()
	if s.DB != nil {
		// Try to resolve store from the tenant context if available
		branding = email.LoadBranding(s.DB, storeID)
	}

	htmlBody := email.RenderRewardEmail(branding, customerName, rewardDescription)

	mailDoc := map[string]interface{}{
		"to": []string{customerEmail},
		"message": map[string]interface{}{
			"subject": fmt.Sprintf("🎉 %s — You Earned a Reward at %s!", customerName, branding.BrandName),
			"html":    htmlBody,
		},
	}

	_, _, err := fsClient.Collection("mail").Add(ctx, mailDoc)
	if err != nil {
		log.Printf("Loyalty Email: Failed to queue for %s: %v", customerEmail, err)
	} else {
		log.Printf("Loyalty Email: Queued reward notification for %s", customerEmail)
	}
}

// HandleDispatchReward allows admins to manually dispatch a reward email to a customer.
// POST /api/v1/crm/dispatch-reward
// Body: { "email": "customer@example.com", "name": "John", "type": "reward|winback" }
func (s *Server) HandleDispatchReward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		Type  string `json:"type"` // "reward" or "winback"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_payload"}`, http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Name == "" {
		http.Error(w, `{"error":"missing_fields"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Load tenant branding
	branding := email.DefaultBranding()
	if s.DB != nil {
		storeID := tc.Config.StoreID
		branding = email.LoadBranding(s.DB, storeID)
	}

	var subject, htmlBody string

	if req.Type == "winback" {
		subject = fmt.Sprintf("We miss you, %s! Here is 10%% off ❤️", req.Name)
		htmlBody = email.RenderWinBackEmail(branding, req.Name, "Enjoy 10% off your next order.")
	} else {
		subject = fmt.Sprintf("🎉 %s — Check out your reward at %s!", req.Name, branding.BrandName)
		htmlBody = email.RenderRewardEmail(branding, req.Name, "Enjoy a Free Classic Falooda on us!")
	}

	mailDoc := map[string]interface{}{
		"to": []string{req.Email},
		"message": map[string]interface{}{
			"subject": subject,
			"html":    htmlBody,
		},
	}

	_, _, err := tc.Firestore.Collection("mail").Add(ctx, mailDoc)
	if err != nil {
		log.Printf("CRM Email: Failed to queue for %s: %v", req.Email, err)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("CRM Email: Dispatched %s email to %s", req.Type, req.Email)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
}

// normalisePhone strips whitespace and converts UK phone prefixes
func normalisePhone(phone string) string {
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "+44", "0")
	phone = strings.ReplaceAll(phone, "0044", "0")
	return phone
}

// HandleAddLoyaltyPoints adds points for an order and generates £5 reward for every 100 points.
// POST /api/v1/loyalty/:phone/add-points
// Body: { "points": 50 }
func (s *Server) HandleAddLoyaltyPoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, `{"error":"missing_phone"}`, http.StatusBadRequest)
		return
	}
	phone := normalisePhone(parts[4])

	var req struct {
		Points int `json:"points"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_payload"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	docRef := tc.Firestore.Collection(CUSTOMERS_COLLECTION).Doc(phone)

	var rewardsGenerated int
	var customerName, customerEmail string

	err := tc.Firestore.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(docRef)
		if err != nil {
			return err
		}

		data := snap.Data()
		if n, ok := data["name"].(string); ok {
			customerName = n
		}
		if e, ok := data["email"].(string); ok {
			customerEmail = e
		}

		currentPoints := 0
		if v, ok := data["loyaltyPoints"].(int64); ok {
			currentPoints = int(v)
		}

		newPoints := currentPoints + req.Points
		localRewardsGenerated := 0
		var newRewards []interface{}
		if r, ok := data["rewards"].([]interface{}); ok {
			newRewards = r
		}

		// 100 points = £5 off reward
		for newPoints >= 100 {
			newPoints -= 100
			localRewardsGenerated++
			
			reward := map[string]interface{}{
				"id":        fmt.Sprintf("loyalty_5off_%s", time.Now().Format("20060102150405")),
				"type":      "discount_fixed",
				"value":     5.00,
				"reason":    "£5 Off (100 Points Reward)",
				"status":    "available",
				"createdAt": time.Now().Format(time.RFC3339),
				"expiresAt": time.Now().AddDate(0, 1, 0).Format(time.RFC3339), // 1 month expiry
			}
			newRewards = append(newRewards, reward)
		}

		updates := []firestore.Update{
			{Path: "loyaltyPoints", Value: newPoints},
			{Path: "rewards", Value: newRewards},
		}

		rewardsGenerated = localRewardsGenerated
		return tx.Update(docRef, updates)
	})

	if err != nil {
		log.Printf("Loyalty: Failed to add points for %s: %v", phone, err)
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	// Trigger automated milestone email if a reward was unlocked
	if rewardsGenerated > 0 && customerEmail != "" && customerName != "" {
		storeID := tc.Config.StoreID
		go s.SendLoyaltyRewardEmail(context.Background(), tc.Firestore, storeID, customerEmail, customerName, "You've earned £5 Off! Valid for 30 days.")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"message": "Points added successfully",
	})
}

// AddLoyaltyPointsInternal silently adds points to a customer's profile without an HTTP request.
func (s *Server) AddLoyaltyPointsInternal(ctx context.Context, fsClient *firestore.Client, storeID string, phone string, points int) error {
	if fsClient == nil || phone == "" || points <= 0 {
		return nil
	}
	docRef := fsClient.Collection(CUSTOMERS_COLLECTION).Doc(phone)

	var rewardsGenerated int
	var customerName, customerEmail string

	err := fsClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(docRef)
		if err != nil {
			return err // Might not exist yet in Firestore, that's fine, we skip or could create. Wait, if it doesn't exist, we should create it.
		}

		data := snap.Data()
		if n, ok := data["name"].(string); ok {
			customerName = n
		}
		if e, ok := data["email"].(string); ok {
			customerEmail = e
		}

		currentPoints := 0
		if v, ok := data["loyaltyPoints"].(int64); ok {
			currentPoints = int(v)
		}

		newPoints := currentPoints + points
		localRewardsGenerated := 0
		var newRewards []interface{}
		if r, ok := data["rewards"].([]interface{}); ok {
			newRewards = r
		}

		for newPoints >= 100 {
			newPoints -= 100
			localRewardsGenerated++
			
			reward := map[string]interface{}{
				"id":        fmt.Sprintf("loyalty_5off_%s", time.Now().Format("20060102150405")),
				"type":      "discount_fixed",
				"value":     5.00,
				"reason":    "£5 Off (100 Points Reward)",
				"status":    "available",
				"createdAt": time.Now().Format(time.RFC3339),
				"expiresAt": time.Now().AddDate(0, 1, 0).Format(time.RFC3339),
			}
			newRewards = append(newRewards, reward)
		}

		updates := []firestore.Update{
			{Path: "loyaltyPoints", Value: newPoints},
			{Path: "rewards", Value: newRewards},
		}

		rewardsGenerated = localRewardsGenerated
		return tx.Update(docRef, updates)
	})

	if err == nil && rewardsGenerated > 0 && customerEmail != "" && customerName != "" {
		go s.SendLoyaltyRewardEmail(context.Background(), fsClient, storeID, customerEmail, customerName, "You've earned £5 Off! Valid for 30 days.")
	}

	return err
}
