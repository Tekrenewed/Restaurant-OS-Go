package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"restaurant-os/internal/firebase"
)

// GenerateMediaRequest represents the payload from the frontend to start an AI job
type GenerateMediaRequest struct {
	ImageURL      string `json:"imageUrl"`
	Prompt        string `json:"prompt"`
	Model         string `json:"model"`     // "banana" or "veo3"
	Type          string `json:"type"`      // "image" or "video"
	Style         string `json:"style"`
	AspectRatio   string `json:"aspectRatio"`
	Duration      string `json:"duration,omitempty"` // For Veo 3
	TrendId       string `json:"trendId,omitempty"`
}

// GenerateMediaResponse is returned immediately to the frontend
type GenerateMediaResponse struct {
	JobID   string `json:"jobId"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// HandleGenerateAIMedia initiates an asynchronous AI generation job
func (s *Server) HandleGenerateAIMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GenerateMediaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("AI Media decode error: %v", err)
		http.Error(w, `{"error":"invalid_payload","message":"Invalid request format"}`, http.StatusBadRequest)
		return
	}

	// Basic validation
	if req.ImageURL == "" || req.Prompt == "" {
		http.Error(w, `{"error":"missing_fields","message":"imageUrl and prompt are required"}`, http.StatusBadRequest)
		return
	}

	// 1. Get Firestore Client for current tenant
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"server_error","message":"Firestore client not found"}`, http.StatusInternalServerError)
		return
	}

	// 2. Generate Job ID
	jobID := fmt.Sprintf("job_%s", uuid.New().String())

	// 3. Create initial Firestore Document in `ai_media_library`
	docData := map[string]interface{}{
		"id":               jobID,
		"type":             req.Type,
		"status":           "processing",
		"originalImageRef": req.ImageURL,
		"prompt":           req.Prompt,
		"model":            req.Model,
		"style":            req.Style,
		"aspectRatio":      req.AspectRatio,
		"trendId":          req.TrendId,
		"createdAt":        time.Now().UnixMilli(),
		"finalMediaUrl":    nil,
		"caption":          nil,
		"hashtags":         nil,
	}

	_, err := tc.Firestore.Collection("ai_media_library").Doc(jobID).Set(r.Context(), docData)
	if err != nil {
		log.Printf("Failed to create AI job in Firestore: %v", err)
		http.Error(w, `{"error":"server_error","message":"Failed to initialize job"}`, http.StatusInternalServerError)
		return
	}

	// 4. Dispatch Background Worker
	// We pass context.Background() because the request context `r.Context()` will be cancelled
	// as soon as this HTTP handler returns, which would kill the goroutine.
	go s.processAIMediaJob(context.Background(), tc, jobID, req)

	// 5. Return immediately
	resp := GenerateMediaResponse{
		JobID:   jobID,
		Status:  "processing",
		Message: "AI generation started successfully. Listen to Firestore for completion.",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted) // 202 Accepted
	json.NewEncoder(w).Encode(resp)
}

// processAIMediaJob simulates the long-running API calls to Banana.dev/Google Vertex
func (s *Server) processAIMediaJob(ctx context.Context, tc *firebase.TenantClient, jobID string, req GenerateMediaRequest) {
	log.Printf("[AI JOB STARTED] JobID: %s, Model: %s, Type: %s", jobID, req.Model, req.Type)
	var finalMediaURL string
	var err error

	// 1. Dispatch to specific API Provider
	if req.Model == "banana" {
		finalMediaURL, err = callNanoBananaAPI(req.Prompt)
	} else if req.Model == "veo3" {
		motionPrompt := "cinematic pan, ultra-detailed food photography style"
		if req.Style != "" {
			motionPrompt = req.Style
		}
		finalMediaURL, err = callGoogleVeo3(req.Prompt, motionPrompt)
	} else {
		err = fmt.Errorf("unsupported model: %s", req.Model)
	}

	if err != nil {
		log.Printf("[AI JOB FAILED] JobID: %s. Error: %v", jobID, err)
		tc.Firestore.Collection("ai_media_library").Doc(jobID).Set(ctx, map[string]interface{}{
			"status": "failed",
			"error":  err.Error(),
		}, firestore.MergeAll)
		return
	}

	// 2. Generate Caption and Hashtags using Google Pro AI
	systemContext := "You are a top-tier social media manager for a premium dessert brand called Falooda & Co. Write a short, engaging, viral caption with emojis and hashtags based on the prompt given."
	captionRaw, aiErr := callGoogleProAI(systemContext, req.Prompt)
	caption := "Experience the ultimate indulgence with our latest creation! ✨"
	hashtags := "#FaloodaAndCo #DessertGoals #Foodie #Trending"
	
	if aiErr == nil && captionRaw != "" {
		caption = captionRaw
	} else {
		log.Printf("[AI JOB WARNING] Could not generate caption via Pro AI: %v", aiErr)
	}

	// 3. Update Firestore with the completed results
	updateData := map[string]interface{}{
		"status":        "ready",
		"finalMediaUrl": finalMediaURL,
		"caption":       caption,
		"hashtags":      hashtags,
		"completedAt":   time.Now().UnixMilli(),
	}

	_, err = tc.Firestore.Collection("ai_media_library").Doc(jobID).Set(ctx, updateData, firestore.MergeAll)
	if err != nil {
		log.Printf("[AI JOB FAILED] JobID: %s. Error updating Firestore: %v", jobID, err)
		return
	}

	log.Printf("[AI JOB COMPLETED] JobID: %s", jobID)
}

// HandleGetViralTrends returns the currently cached viral trends
func (s *Server) HandleGetViralTrends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"server_error","message":"Firestore client not found"}`, http.StatusInternalServerError)
		return
	}

	// Fetch from the viral_trends_cache collection
	docs, err := tc.Firestore.Collection("viral_trends_cache").OrderBy("score", firestore.Desc).Limit(10).Documents(r.Context()).GetAll()
	if err != nil {
		log.Printf("Failed to fetch viral trends: %v", err)
		http.Error(w, `{"error":"server_error","message":"Failed to fetch trends"}`, http.StatusInternalServerError)
		return
	}

	var trends []map[string]interface{}
	for _, doc := range docs {
		data := doc.Data()
		data["id"] = doc.Ref.ID
		trends = append(trends, data)
	}

	// If the cache is empty (e.g. cron hasn't run yet), return a fallback so the UI doesn't break
	if len(trends) == 0 {
		trends = []map[string]interface{}{
			{
				"id": "fallback-1",
				"name": "ASMR Chocolate Pour",
				"type": "Audio Hook",
				"promptModifier": "Extreme macro close up, 4k, cinematic lighting, slow motion pour, rich dark chocolate, highly detailed texture, professional food photography, dark background, appetizing",
				"score": 98,
			},
			{
				"id": "fallback-2",
				"name": "Fast-Cut Kitchen BTS",
				"type": "Editing Style",
				"promptModifier": "Dynamic angle, motion blur, busy kitchen background, bright neon lighting, high contrast, vibrant colors, energetic feel",
				"score": 92,
			},
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trends)
}

// HandleScrapeViralTrendsCron is triggered by Cloud Scheduler to scrape real trends
// and update the `viral_trends_cache` collection.
func (s *Server) HandleScrapeViralTrendsCron(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"server_error","message":"Firestore client not found"}`, http.StatusInternalServerError)
		return
	}

	// 1. Call Google Pro AI to analyze current social media signals
	log.Println("[CRON] Scraping viral trends via Google Pro AI...")
	
	systemContext := "You are a top-tier data analyst for a premium dessert brand called Falooda & Co. Analyze current global food trends on TikTok and Instagram. Provide exactly 3 distinct trends in a raw JSON array format with keys: 'name' (string), 'type' (string, e.g. Audio Hook, Visual Flow), 'promptModifier' (string, a highly descriptive comma-separated string for an image/video generation prompt), and 'score' (number between 80-99)."
	trendJSONRaw, err := callGoogleProAI(systemContext, "What are today's top 3 dessert media trends?")
	
	// Strip markdown blocks if present (e.g. ```json ... ```)
	if len(trendJSONRaw) > 7 && trendJSONRaw[:7] == "```json" {
		trendJSONRaw = trendJSONRaw[7 : len(trendJSONRaw)-3]
	} else if len(trendJSONRaw) > 3 && trendJSONRaw[:3] == "```" {
		trendJSONRaw = trendJSONRaw[3 : len(trendJSONRaw)-3]
	}

	var newTrends []map[string]interface{}
	if err == nil {
		err = json.Unmarshal([]byte(trendJSONRaw), &newTrends)
	}

	if err != nil || len(newTrends) == 0 {
		log.Printf("[CRON WARNING] Failed to parse AI trends (%v). Falling back to defaults.", err)
		newTrends = []map[string]interface{}{
			{
				"name": "ASMR Chocolate Pour",
				"type": "Audio Hook",
				"promptModifier": "Extreme macro close up, high framerate slow motion pour, emphasizing rich textures, deep shadows, 4k resolution.",
				"score": 98,
			},
			{
				"name": "Fast-Cut Kitchen BTS",
				"type": "Editing Style",
				"promptModifier": "Dynamic camera movement, rhythmic cuts, vibrant neon lighting accents, energetic mood, food preparation.",
				"score": 94,
			},
			{
				"name": "The 'First Bite' Pull",
				"type": "Visual Flow",
				"promptModifier": "Shallow depth of field, focused on dessert texture pull, warm inviting lighting, cinematic portrait mode.",
				"score": 89,
			},
		}
	}
	
	// Append timestamps
	for i := range newTrends {
		newTrends[i]["lastUpdated"] = time.Now().UnixMilli()
	}

	// 2. Overwrite the cache in Firestore
	batch := tc.Firestore.Batch()
	
	// Delete existing cache (simplified: in a real scenario we'd query and delete all)
	// For now we just overwrite 3 specific docs
	batch.Set(tc.Firestore.Collection("viral_trends_cache").Doc("t1"), newTrends[0])
	batch.Set(tc.Firestore.Collection("viral_trends_cache").Doc("t2"), newTrends[1])
	batch.Set(tc.Firestore.Collection("viral_trends_cache").Doc("t3"), newTrends[2])

	_, err = batch.Commit(r.Context())
	if err != nil {
		log.Printf("[CRON] Failed to update viral trends cache: %v", err)
		http.Error(w, `{"error":"server_error","message":"Failed to update cache"}`, http.StatusInternalServerError)
		return
	}

	log.Println("[CRON] Viral trends cache updated successfully.")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
