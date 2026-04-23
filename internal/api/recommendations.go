package api

import (
	"encoding/json"
	"log"
	"net/http"
)

// ─── Product Recommendation Engine ───
// Analyses purchase patterns to identify frequently bought-together items
// and trending products. Triggered by Cloud Scheduler weekly.
// POST /api/v1/internal/build-recommendations

// HandleBuildRecommendations analyses order data to build co-purchase recommendations.
// For each product, it finds what other products are frequently ordered in the same order.
func (s *Server) HandleBuildRecommendations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.DB == nil {
		http.Error(w, `{"error":"database_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	// Find co-purchase patterns: items frequently ordered together
	// Uses the relational order_items table joined to orders for store_id
	rows, err := s.DB.Query(`
		WITH pairs AS (
			SELECT 
				o.store_id,
				a.name AS product_a,
				b.name AS product_b,
				COUNT(*) AS pair_count
			FROM order_items a
			JOIN order_items b ON a.order_id = b.order_id AND a.name < b.name
			JOIN orders o ON o.id = a.order_id
			WHERE o.store_id IS NOT NULL
			GROUP BY o.store_id, a.name, b.name
			HAVING COUNT(*) >= 2
		)
		SELECT store_id::text, product_a, product_b, pair_count
		FROM pairs
		ORDER BY pair_count DESC
		LIMIT 100
	`)
	if err != nil {
		log.Printf("Recommendations: Co-purchase query failed: %v", err)
		http.Error(w, `{"error":"query_failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	inserted := 0
	for rows.Next() {
		var storeID, productA, productB string
		var pairCount int
		if err := rows.Scan(&storeID, &productA, &productB, &pairCount); err != nil {
			log.Printf("Recommendations: Scan error: %v", err)
			continue
		}

		// Upsert the recommendation (bidirectional — A recommends B and B recommends A)
		for _, pair := range [][2]string{{productA, productB}, {productB, productA}} {
			_, err := s.DB.Exec(`
				INSERT INTO product_recommendations (store_id, source_product, recommended_product, confidence_score)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (store_id, source_product, recommended_product) DO UPDATE SET
					confidence_score = EXCLUDED.confidence_score,
					updated_at = NOW()
			`, storeID, pair[0], pair[1], pairCount)
			if err != nil {
				log.Printf("Recommendations: Failed to upsert %s→%s: %v", pair[0], pair[1], err)
			} else {
				inserted++
			}
		}
	}

	// Also find trending products (most ordered in last 30 days)
	trendingRows, err := s.DB.Query(`
		SELECT 
			o.store_id::text,
			oi.name AS product_name,
			COUNT(*) AS order_count
		FROM order_items oi
		JOIN orders o ON o.id = oi.order_id
		WHERE o.created_at >= NOW() - INTERVAL '30 days'
		  AND o.store_id IS NOT NULL
		GROUP BY o.store_id, oi.name
		HAVING COUNT(*) >= 3
		ORDER BY COUNT(*) DESC
		LIMIT 20
	`)
	if err != nil {
		log.Printf("Recommendations: Trending query failed: %v", err)
	} else {
		defer trendingRows.Close()
		for trendingRows.Next() {
			var storeID, productName string
			var orderCount int
			if err := trendingRows.Scan(&storeID, &productName, &orderCount); err != nil {
				continue
			}
			// Store trending item as a self-referencing recommendation with high score
			_, _ = s.DB.Exec(`
				INSERT INTO product_recommendations (store_id, source_product, recommended_product, confidence_score)
				VALUES ($1, $2, $2, $3)
				ON CONFLICT (store_id, source_product, recommended_product) DO UPDATE SET
					confidence_score = EXCLUDED.confidence_score,
					updated_at = NOW()
			`, storeID, productName, orderCount*10)
		}
	}

	result := map[string]interface{}{
		"status":              "complete",
		"recommendations_added": inserted,
	}

	log.Printf("Recommendations build complete: %d pairs inserted", inserted)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// HandleGetRecommendations returns product recommendations for a given product.
// GET /api/v1/recommendations?product=xxx&store_id=xxx
func (s *Server) HandleGetRecommendations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.DB == nil {
		http.Error(w, `{"error":"database_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	product := r.URL.Query().Get("product")
	storeID := r.URL.Query().Get("store_id")

	type Recommendation struct {
		Product    string `json:"product" db:"recommended_product"`
		Confidence int    `json:"confidence" db:"confidence_score"`
	}

	var recs []Recommendation

	if product != "" && storeID != "" {
		// Specific product recommendations
		err := s.DB.Select(&recs, `
			SELECT recommended_product, confidence_score
			FROM product_recommendations
			WHERE source_product = $1 AND store_id = $2 AND source_product != recommended_product
			ORDER BY confidence_score DESC
			LIMIT 5
		`, product, storeID)
		if err != nil {
			log.Printf("Recommendations: Query failed: %v", err)
		}
	} else if storeID != "" {
		// Trending products for the store (self-referencing entries)
		err := s.DB.Select(&recs, `
			SELECT recommended_product, confidence_score
			FROM product_recommendations
			WHERE store_id = $1 AND source_product = recommended_product
			ORDER BY confidence_score DESC
			LIMIT 10
		`, storeID)
		if err != nil {
			log.Printf("Recommendations: Trending query failed: %v", err)
		}
	}

	if recs == nil {
		recs = []Recommendation{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"product":         product,
		"recommendations": recs,
	})
}
