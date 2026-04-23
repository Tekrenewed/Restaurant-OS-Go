package api

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"time"
)

// ─── RFM Customer Scoring Engine ───
// Triggered nightly by Cloud Scheduler: POST /api/v1/internal/score-customers
// Calculates Recency (40pts), Frequency (30pts), Monetary (30pts) for each customer
// and assigns segments: VIP, REGULAR, CHURN_RISK, NEW

// customerRawData holds the raw metrics pulled from orders for RFM calculation
type customerRawData struct {
	CustomerID string    `db:"customer_id"`
	StoreID    string    `db:"store_id"`
	LastOrder  time.Time `db:"last_order"`
	OrderCount int       `db:"order_count"`
	AvgSpend   float64   `db:"avg_spend"`
	TotalSpent float64   `db:"total_spent"`
}

// HandleRecalculateScores recalculates RFM scores for all customers across all stores.
// POST /api/v1/internal/score-customers (protected by MigrateKeyMiddleware)
func (s *Server) HandleRecalculateScores(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.DB == nil {
		http.Error(w, `{"error":"database_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	// Pull raw order metrics for every customer
	// Links customers to orders via phone number (the universal identifier)
	// Uses order's store_id if customer doesn't have one (migrated customers)
	var customers []customerRawData
	err := s.DB.Select(&customers, `
		SELECT 
			c.id::text AS customer_id,
			COALESCE(c.store_id, o.store_id)::text AS store_id,
			COALESCE(MAX(o.created_at), c.created_at) AS last_order,
			COUNT(o.id) AS order_count,
			COALESCE(AVG(o.gross_total), 0) AS avg_spend,
			COALESCE(SUM(o.gross_total), 0) AS total_spent
		FROM customers c
		LEFT JOIN orders o ON o.customer_phone = c.phone
		GROUP BY c.id, c.store_id, o.store_id, c.created_at
		HAVING COALESCE(c.store_id, o.store_id) IS NOT NULL
	`)
	if err != nil {
		log.Printf("Scoring: Failed to query customer metrics: %v", err)
		http.Error(w, `{"error":"query_failed"}`, http.StatusInternalServerError)
		return
	}

	now := time.Now()
	scored := 0
	segmentCounts := map[string]int{"VIP": 0, "REGULAR": 0, "CHURN_RISK": 0, "NEW": 0}

	for _, c := range customers {
		if c.StoreID == "" {
			continue
		}

		// ─── Recency Score (0-40) ───
		// How recently did they last order?
		daysSinceLastOrder := now.Sub(c.LastOrder).Hours() / 24
		var recencyScore int
		switch {
		case daysSinceLastOrder <= 7:
			recencyScore = 40 // Ordered this week
		case daysSinceLastOrder <= 14:
			recencyScore = 35
		case daysSinceLastOrder <= 30:
			recencyScore = 25
		case daysSinceLastOrder <= 60:
			recencyScore = 15
		case daysSinceLastOrder <= 90:
			recencyScore = 8
		default:
			recencyScore = 2 // Haven't ordered in 90+ days
		}

		// ─── Frequency Score (0-30) ───
		// How often do they order?
		var frequencyScore int
		switch {
		case c.OrderCount >= 20:
			frequencyScore = 30 // Power user
		case c.OrderCount >= 10:
			frequencyScore = 25
		case c.OrderCount >= 5:
			frequencyScore = 20
		case c.OrderCount >= 3:
			frequencyScore = 15
		case c.OrderCount >= 2:
			frequencyScore = 10
		default:
			frequencyScore = 5 // Single order
		}

		// ─── Monetary Score (0-30) ───
		// How much do they spend on average?
		var monetaryScore int
		switch {
		case c.AvgSpend >= 25:
			monetaryScore = 30 // High spender
		case c.AvgSpend >= 15:
			monetaryScore = 25
		case c.AvgSpend >= 10:
			monetaryScore = 20
		case c.AvgSpend >= 7:
			monetaryScore = 15
		case c.AvgSpend >= 4:
			monetaryScore = 10
		default:
			monetaryScore = 5
		}

		totalScore := recencyScore + frequencyScore + monetaryScore

		// ─── Segment Assignment ───
		segment := assignSegment(totalScore, c.OrderCount, daysSinceLastOrder)
		segmentCounts[segment]++

		// Upsert the score into customer_scores table
		_, err := s.DB.Exec(`
			INSERT INTO customer_scores (customer_id, store_id, recency_score, frequency_score, monetary_score, total_score, segment, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
			ON CONFLICT (customer_id) DO UPDATE SET
				store_id = EXCLUDED.store_id,
				recency_score = EXCLUDED.recency_score,
				frequency_score = EXCLUDED.frequency_score,
				monetary_score = EXCLUDED.monetary_score,
				total_score = EXCLUDED.total_score,
				segment = EXCLUDED.segment,
				updated_at = NOW()
		`, c.CustomerID, c.StoreID, recencyScore, frequencyScore, monetaryScore, totalScore, segment)

		if err != nil {
			log.Printf("Scoring: Failed to upsert score for customer %s: %v", c.CustomerID, err)
			continue
		}

		// Also update the loyalty_points in the customers table from Firestore total
		// (sync Postgres loyalty column with actual order-based calculation)
		loyaltyPoints := int(math.Round(c.TotalSpent)) // £1 spent = 1 point (simplified)
		_, _ = s.DB.Exec(`UPDATE customers SET loyalty_points = $1 WHERE id = $2`, loyaltyPoints, c.CustomerID)

		scored++
	}

	result := map[string]interface{}{
		"status":     "complete",
		"scored":     scored,
		"vip":        segmentCounts["VIP"],
		"regular":    segmentCounts["REGULAR"],
		"churn_risk": segmentCounts["CHURN_RISK"],
		"new":        segmentCounts["NEW"],
	}

	log.Printf("RFM Scoring complete: %d scored | VIP=%d REGULAR=%d CHURN_RISK=%d NEW=%d",
		scored, segmentCounts["VIP"], segmentCounts["REGULAR"], segmentCounts["CHURN_RISK"], segmentCounts["NEW"])

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// assignSegment determines the customer segment based on their total RFM score,
// order count, and days since last order.
func assignSegment(totalScore, orderCount int, daysSinceLastOrder float64) string {
	// New customers: only 1 order, regardless of score
	if orderCount <= 1 {
		return "NEW"
	}

	// Churn risk: haven't ordered in 30+ days AND moderate history
	if daysSinceLastOrder > 30 && orderCount >= 2 {
		return "CHURN_RISK"
	}

	// VIP: high total score (frequent + recent + high spend)
	if totalScore >= 70 {
		return "VIP"
	}

	// Regular: decent engagement
	if totalScore >= 40 {
		return "REGULAR"
	}

	// Default: low engagement but not necessarily churning
	return "NEW"
}
