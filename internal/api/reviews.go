package api

import (
	"context"
	"fmt"
	"log"
	"restaurant-os/internal/email"
	"cloud.google.com/go/firestore"
)

// SendReviewPromptEmail sends a thank-you email with a Google Review link
// after an order is marked as "completed".
// Triggered automatically from HandleUpdateOrderStatus.
// Now tenant-aware: loads branding from the email_templates table.
func (s *Server) SendReviewPromptEmail(ctx context.Context, fsClient *firestore.Client, orderID string) {
	if fsClient == nil {
		return
	}
	if s.DB == nil {
		return
	}

	// Look up customer info from the order
	var customerName *string
	var customerPhone *string
	var storeID *string
	err := s.DB.QueryRow(
		`SELECT customer_name, customer_phone, store_id FROM orders WHERE id = $1`, orderID,
	).Scan(&customerName, &customerPhone, &storeID)
	if err != nil {
		log.Printf("Review Prompt: Could not find order %s: %v", orderID, err)
		return
	}

	// Need a phone number to look up the customer email
	if customerPhone == nil || *customerPhone == "" {
		return
	}

	// Look up customer email from Firestore customers collection
	phone := normalisePhone(*customerPhone)
	docSnap, err := fsClient.Collection("customers").Doc(phone).Get(ctx)
	if err != nil {
		// Customer doesn't exist in loyalty system — skip
		return
	}

	data := docSnap.Data()
	customerEmail, _ := data["email"].(string)
	if customerEmail == "" {
		// No email on file — can't send review prompt
		return
	}

	name := "Valued Customer"
	if customerName != nil && *customerName != "" {
		name = *customerName
	} else if n, ok := data["name"].(string); ok && n != "" {
		name = n
	}

	// Load tenant-specific branding (falls back to Falooda & Co defaults)
	sid := ""
	if storeID != nil {
		sid = *storeID
	}
	branding := email.LoadBranding(s.DB, sid)

	// Build the review email using the template engine
	htmlBody := email.RenderReviewEmail(branding, name)

	mailDoc := map[string]interface{}{
		"to":   []string{customerEmail},
		"from": fmt.Sprintf("%s <noreply@faloodaandco.co.uk>", branding.BrandName),
		"message": map[string]interface{}{
			"subject": fmt.Sprintf("⭐ %s, how was your %s experience?", name, branding.BrandName),
			"html":    htmlBody,
		},
	}

	_, _, err = fsClient.Collection("mail").Add(ctx, mailDoc)
	if err != nil {
		log.Printf("Review Prompt: Failed to queue email for %s: %v", customerEmail, err)
	} else {
		log.Printf("Review Prompt: Queued review email for %s (order %s)", customerEmail, orderID)
	}
}
