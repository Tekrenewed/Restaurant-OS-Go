package api

import (
	"encoding/json"
	"log"
	"net/http"
)

// ─── Server-Side Segments API ───
// Replaces the naive client-side order counting in useMarketing.ts
// with pre-computed RFM segments from the customer_scores table.

// SegmentCustomer represents a customer within a segment
type SegmentCustomer struct {
	ID            string  `json:"id" db:"id"`
	Phone         string  `json:"phone" db:"phone"`
	Email         string  `json:"email" db:"email"`
	Name          string  `json:"name" db:"name"`
	LoyaltyPoints int     `json:"loyalty_points" db:"loyalty_points"`
	TotalScore    int     `json:"total_score" db:"total_score"`
	Segment       string  `json:"segment" db:"segment"`
	RecencyScore  int     `json:"recency_score" db:"recency_score"`
	FrequencyScore int    `json:"frequency_score" db:"frequency_score"`
	MonetaryScore int     `json:"monetary_score" db:"monetary_score"`
}

// SegmentsResponse is the JSON payload returned by the segments endpoint
type SegmentsResponse struct {
	VIP       []SegmentCustomer `json:"vip"`
	Regular   []SegmentCustomer `json:"regular"`
	ChurnRisk []SegmentCustomer `json:"churn_risk"`
	New       []SegmentCustomer `json:"new"`
	Summary   SegmentSummary    `json:"summary"`
}

// SegmentSummary provides quick counts for dashboard cards
type SegmentSummary struct {
	TotalCustomers int `json:"total_customers"`
	VIPCount       int `json:"vip_count"`
	RegularCount   int `json:"regular_count"`
	ChurnRiskCount int `json:"churn_risk_count"`
	NewCount       int `json:"new_count"`
	ScoredAt       string `json:"scored_at"` // Last scoring run timestamp
}

// HandleGetSegments returns pre-computed customer segments for the admin dashboard.
// GET /api/v1/segments?store_id=xxx (auth-protected)
func (s *Server) HandleGetSegments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.DB == nil {
		http.Error(w, `{"error":"database_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	storeID := r.URL.Query().Get("store_id")

	// Build the query — optionally filter by store_id
	query := `
		SELECT 
			c.id, c.phone, COALESCE(c.email, '') as email, COALESCE(c.name, 'Customer') as name,
			c.loyalty_points,
			COALESCE(cs.total_score, 0) as total_score,
			COALESCE(cs.segment, 'NEW') as segment,
			COALESCE(cs.recency_score, 0) as recency_score,
			COALESCE(cs.frequency_score, 0) as frequency_score,
			COALESCE(cs.monetary_score, 0) as monetary_score
		FROM customers c
		LEFT JOIN customer_scores cs ON cs.customer_id = c.id
	`
	args := []interface{}{}

	if storeID != "" {
		query += " WHERE c.store_id = $1 OR cs.store_id = $1"
		args = append(args, storeID)
	}

	query += " ORDER BY COALESCE(cs.total_score, 0) DESC"

	var customers []SegmentCustomer
	var err error
	if len(args) > 0 {
		err = s.DB.Select(&customers, query, args...)
	} else {
		err = s.DB.Select(&customers, query)
	}

	if err != nil {
		log.Printf("Segments: Query failed: %v", err)
		http.Error(w, `{"error":"query_failed"}`, http.StatusInternalServerError)
		return
	}

	// Sort into segments
	resp := SegmentsResponse{
		VIP:       []SegmentCustomer{},
		Regular:   []SegmentCustomer{},
		ChurnRisk: []SegmentCustomer{},
		New:       []SegmentCustomer{},
	}

	for _, c := range customers {
		switch c.Segment {
		case "VIP":
			resp.VIP = append(resp.VIP, c)
		case "REGULAR":
			resp.Regular = append(resp.Regular, c)
		case "CHURN_RISK":
			resp.ChurnRisk = append(resp.ChurnRisk, c)
		default:
			resp.New = append(resp.New, c)
		}
	}

	// Get the last scoring timestamp
	var scoredAt *string
	_ = s.DB.Get(&scoredAt, `SELECT MAX(updated_at)::text FROM customer_scores`)
	scoredAtStr := "never"
	if scoredAt != nil {
		scoredAtStr = *scoredAt
	}

	resp.Summary = SegmentSummary{
		TotalCustomers: len(customers),
		VIPCount:       len(resp.VIP),
		RegularCount:   len(resp.Regular),
		ChurnRiskCount: len(resp.ChurnRisk),
		NewCount:       len(resp.New),
		ScoredAt:       scoredAtStr,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleGetCustomerScore returns the RFM score for a single customer.
// GET /api/v1/customers/:phone/score
func (s *Server) HandleGetCustomerScore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.DB == nil {
		http.Error(w, `{"error":"database_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	phone := r.URL.Query().Get("phone")
	if phone == "" {
		http.Error(w, `{"error":"phone_required"}`, http.StatusBadRequest)
		return
	}
	phone = normalisePhone(phone)

	var customer SegmentCustomer
	err := s.DB.Get(&customer, `
		SELECT 
			c.id, c.phone, COALESCE(c.email, '') as email, COALESCE(c.name, 'Customer') as name,
			c.loyalty_points,
			COALESCE(cs.total_score, 0) as total_score,
			COALESCE(cs.segment, 'NEW') as segment,
			COALESCE(cs.recency_score, 0) as recency_score,
			COALESCE(cs.frequency_score, 0) as frequency_score,
			COALESCE(cs.monetary_score, 0) as monetary_score
		FROM customers c
		LEFT JOIN customer_scores cs ON cs.customer_id = c.id
		WHERE c.phone = $1
		LIMIT 1
	`, phone)

	if err != nil {
		http.Error(w, `{"error":"customer_not_found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(customer)
}
