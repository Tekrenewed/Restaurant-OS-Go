package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"restaurant-os/pkg/aggregator"
	"restaurant-os/pkg/database"
	"restaurant-os/pkg/events"
)

type Application struct {
	DB     *database.DBWrapper
	PubSub events.PubSubClient
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	// 1. Initialize PostgreSQL Connection Pool
	cfg := database.Config{
		Host:     os.Getenv("DB_HOST"),
		Port:     os.Getenv("DB_PORT"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PASS"),
		DBName:   os.Getenv("DB_NAME"),
		SSLMode:  "disable", // Use "require" in production
	}
	
	dbPool, err := database.NewConnectionPool(context.Background(), cfg)
	if err != nil {
		log.Printf("Warning: Failed to connect to DB: %v. Running in degraded mode.", err)
	} else {
		defer dbPool.Close()
	}

	// 2. Initialize Event Bus (Redis / Local Adapter)
	eventBus := events.NewLocalPubSub()

	app := &Application{
		DB:     dbPool,
		PubSub: eventBus,
	}

	http.HandleFunc("/webhook/ubereats", app.handleUberEatsWebhook)
	http.HandleFunc("/webhook/justeat", app.handleJustEatWebhook)

	log.Printf("svc-aggregator starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

func (app *Application) handleUberEatsWebhook(w http.ResponseWriter, r *http.Request) {
	// 1. Verify HMAC Signature (Security)
	// Uber Eats sends signature in "x-uber-signature" header
	signature := r.Header.Get("x-uber-signature")
	secret := os.Getenv("UBER_EATS_WEBHOOK_SECRET")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Unable to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if secret != "" {
		if !aggregator.VerifySignature(secret, body, signature) {
			log.Println("WARNING: Invalid Uber Eats webhook signature")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// In production, we extract the tenant ID from the Uber Eats Store ID mapping
	tenantID := "taste-of-village" 

	// Parse payload into standard OS format
	order, err := aggregator.NormalizeUberEats(r.Context(), app.DB, body, tenantID)
	if err != nil {
		log.Printf("Normalization error: %v", err)
		http.Error(w, "Bad Payload", http.StatusBadRequest)
		return
	}

	// 2. Idempotency Check (No Double-Printing)
	err = database.CheckIdempotency(r.Context(), app.DB, tenantID, order.ExternalID, "UBEREATS")
	if err == database.ErrOrderAlreadyExists {
		log.Printf("Idempotency: Order %s already processed. Skipping.", order.ExternalID)
		w.WriteHeader(http.StatusOK) // Acknowledge to Uber Eats so they stop retrying
		return
	} else if err != nil {
		log.Printf("DB Error checking idempotency: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Save to Postgres
	if app.DB != nil {
		log.Printf("Saving Order for tenant [%s] with Total: £%.2f", order.TenantID, order.TotalAmount)
		// app.DB.ExecContext(r.Context(), "INSERT INTO orders ...") 
	}

	// Fire Event to hardware daemon so kitchen Epson prints it
	payloadBytes, _ := json.Marshal(order)
	err = app.PubSub.Publish(r.Context(), events.Event{
		Topic:    "PRINT_JOB",
		TenantID: tenantID,
		Payload:  payloadBytes,
	})
	if err != nil {
		log.Printf("Failed to publish print event: %v", err)
	} else {
		log.Println("Published PRINT_JOB event successfully.")
	}

	w.WriteHeader(http.StatusOK)
}

func (app *Application) handleJustEatWebhook(w http.ResponseWriter, r *http.Request) {
	log.Println("Received Just Eat webhook")
	w.WriteHeader(http.StatusOK)
}
