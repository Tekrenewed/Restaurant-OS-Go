package integrations

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"restaurant-os/internal/models"

	"github.com/google/uuid"
)

// DojoClient handles the Dojo Cloud API integration for automated card payments.
// Instead of staff manually keying amounts into the terminal, the POS sends the
// exact basket total through this client, which wakes up the physical Dojo machine.
type DojoClient struct {
	BaseURL    string
	APIKey     string
	APIVersion string
}

// NewDojoClient initializes the Dojo Cloud Payment client.
// Uses sandbox URL by default; production URL is the same base but with a prod key.
func NewDojoClient() *DojoClient {
	baseURL := os.Getenv("DOJO_API_URL")
	if baseURL == "" {
		baseURL = "https://api.dojo.tech"
	}

	apiVersion := os.Getenv("DOJO_API_VERSION")
	if apiVersion == "" {
		apiVersion = "2024-02-27"
	}

	return &DojoClient{
		BaseURL:    baseURL,
		APIKey:     os.Getenv("DOJO_API_KEY"),
		APIVersion: apiVersion,
	}
}

// --- Request/Response Shapes (matching Dojo's official schema) ---

// DojoAmount represents the monetary value in the smallest currency unit (pence for GBP).
type DojoAmount struct {
	Value        int64  `json:"value"`        // e.g., 1499 = £14.99
	CurrencyCode string `json:"currencyCode"` // "GBP"
}

// DojoPaymentIntentRequest is the payload sent to POST /payment-intents
type DojoPaymentIntentRequest struct {
	Amount      DojoAmount `json:"amount"`
	Reference   string     `json:"reference"`   // Our internal order ID
	Description string     `json:"description"` // Human-readable label shown on terminal
	CaptureMode string     `json:"captureMode"` // "Auto" for instant capture
}

// DojoPaymentIntentResponse is what Dojo returns after creating an intent
type DojoPaymentIntentResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// DojoWebhookPayload is the shape Dojo sends to our webhook when a payment status changes
type DojoWebhookPayload struct {
	ID     string `json:"id"`     // The payment intent ID
	Status string `json:"status"` // e.g., "Captured", "Authorized", "Canceled"
	Amount struct {
		Value        int64  `json:"value"`
		CurrencyCode string `json:"currencyCode"`
	} `json:"amount"`
	Reference string `json:"reference"` // This is our order ID that we sent originally
}

// CreatePaymentIntent initiates a transaction on the physical card terminal.
// This is the core function: it sends the basket total to Dojo's cloud, which
// then wakes up the specific terminal at the counter with the exact amount.
func (c *DojoClient) CreatePaymentIntent(order models.InternalOrder, terminalID string) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("DOJO_API_KEY environment variable is not set")
	}

	// Dojo requires amounts in pence (e.g., £14.99 -> 1499)
	amountInPence := int64(order.GrossTotal * 100)

	reqBody := DojoPaymentIntentRequest{
		Amount: DojoAmount{
			Value:        amountInPence,
			CurrencyCode: "GBP",
		},
		Reference:   order.ID.String(),
		Description: fmt.Sprintf("Falooda & Co - Order %s", order.ID.String()[:8]),
		CaptureMode: "Auto", // Capture immediately on tap — no manual settlement needed
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal dojo request: %w", err)
	}

	url := fmt.Sprintf("%s/payment-intents", c.BaseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Authorization", "Basic "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("version", c.APIVersion)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("dojo api connection error: %w", err)
	}
	defer resp.Body.Close()

	// Read the body for debugging
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		log.Printf("Dojo rejected payment intent. Status: %d, Body: %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("dojo rejected payment intent with status: %d", resp.StatusCode)
	}

	var dojoResp DojoPaymentIntentResponse
	if err := json.Unmarshal(body, &dojoResp); err != nil {
		return "", fmt.Errorf("failed to decode dojo response: %w", err)
	}

	log.Printf("Dojo Payment Intent created: %s (Status: %s) for Order: %s", dojoResp.ID, dojoResp.Status, order.ID.String())
	return dojoResp.ID, nil
}

// VerifyWebhookSignature confirms the webhook payload is genuinely from Dojo
// by computing HMAC-SHA256 of the raw body with our webhook secret.
func VerifyWebhookSignature(body []byte, signatureHeader string) bool {
	secret := os.Getenv("DOJO_WEBHOOK_SECRET")
	if secret == "" {
		log.Println("WARNING: DOJO_WEBHOOK_SECRET not set — accepting webhook without verification")
		return true // Allow in dev, but log the warning
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expectedMAC), []byte(signatureHeader))
}

// LookupTerminalID fetches the Dojo Terminal ID for a given store from Postgres.
// Returns empty string if no terminal is configured (staff should use standalone mode).
func LookupTerminalID(db interface{ Get(dest interface{}, query string, args ...interface{}) error }, storeID uuid.UUID) string {
	var terminalID *string
	err := db.Get(&terminalID, `SELECT dojo_terminal_id FROM stores WHERE id = $1`, storeID)
	if err != nil || terminalID == nil {
		log.Printf("No Dojo terminal configured for store %s — falling back to manual mode", storeID)
		return ""
	}
	return *terminalID
}

// GetPaymentIntent fetches the current status of a payment intent from Dojo's API.
// This is used for manual reconciliation if a webhook is missed.
func (c *DojoClient) GetPaymentIntent(intentID string) (*DojoPaymentIntentResponse, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("DOJO_API_KEY environment variable is not set")
	}

	url := fmt.Sprintf("%s/payment-intents/%s", c.BaseURL, intentID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Authorization", "Basic "+c.APIKey)
	req.Header.Set("version", c.APIVersion)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dojo api connection error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dojo api returned status: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var dojoResp DojoPaymentIntentResponse
	if err := json.Unmarshal(body, &dojoResp); err != nil {
		return nil, fmt.Errorf("failed to decode dojo response: %w", err)
	}

	return &dojoResp, nil
}
