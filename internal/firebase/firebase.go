package firebase

import (
	"context"
	"log"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
	"restaurant-os/internal/circuitbreaker"
)

// Client holds Firebase services used across the application
type Client struct {
	Auth      *auth.Client
	Firestore *firestore.Client
	Messaging *messaging.Client
	cb        *circuitbreaker.CircuitBreaker
}

// InitFirebase initialises Firebase Admin SDK.
// On Cloud Run, it uses Application Default Credentials automatically.
// Locally, set GOOGLE_APPLICATION_CREDENTIALS to a service account JSON.
func InitFirebase() *Client {
	ctx := context.Background()

	var app *firebase.App
	var err error

	// If a service account key file is specified, use it (local dev)
	if credFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); credFile != "" {
		opt := option.WithCredentialsFile(credFile)
		app, err = firebase.NewApp(ctx, nil, opt)
	} else {
		// On Cloud Run, ADC is automatic — no config needed
		app, err = firebase.NewApp(ctx, nil)
	}

	if err != nil {
		log.Printf("WARNING: Firebase init failed: %v (Firestore real-time sync disabled)", err)
		return nil
	}

	authClient, err := app.Auth(ctx)
	if err != nil {
		log.Printf("WARNING: Firebase Auth init failed: %v", err)
	}

	fsClient, err := app.Firestore(ctx)
	if err != nil {
		log.Printf("WARNING: Firestore init failed: %v", err)
	}

	msgClient, err := app.Messaging(ctx)
	if err != nil {
		log.Printf("WARNING: Firebase Messaging init failed: %v", err)
	}

	log.Println("Firebase Admin SDK initialised successfully")
	return &Client{
		Auth:      authClient,
		Firestore: fsClient,
		Messaging: msgClient,
		cb:        circuitbreaker.New(3, 10*time.Second), // 3 failures triggers open state for 10s
	}
}

// PushActiveOrder writes an order to Firestore's "active_orders" collection.
// This is what triggers the real-time update on POS and KDS screens.
func (c *Client) PushActiveOrder(ctx context.Context, orderID string, data map[string]interface{}) error {
	if c == nil || c.Firestore == nil {
		log.Println("Firestore not available, skipping real-time push")
		return nil
	}

	err := c.cb.Execute(func() error {
		_, e := c.Firestore.Collection("active_orders").Doc(orderID).Set(ctx, data)
		return e
	})

	if err != nil {
		if err == circuitbreaker.ErrCircuitOpen {
			log.Printf("WARNING: Circuit breaker open, skipped PushActiveOrder for %s", orderID)
			return nil // degraded mode, don't fail the HTTP request
		}
		log.Printf("ERROR: Failed to push order %s to Firestore: %v", orderID, err)
		return err
	}

	log.Printf("Order %s pushed to Firestore active_orders (POS/KDS will update)", orderID)
	return nil
}

// PushToOrders writes an order to the primary "orders" collection for Web backwards compatibility
func (c *Client) PushToOrders(ctx context.Context, orderID string, data map[string]interface{}) error {
	if c == nil || c.Firestore == nil {
		return nil
	}

	err := c.cb.Execute(func() error {
		_, e := c.Firestore.Collection("orders").Doc(orderID).Set(ctx, data)
		return e
	})

	if err != nil {
		if err == circuitbreaker.ErrCircuitOpen {
			log.Printf("WARNING: Circuit breaker open, skipped PushToOrders for %s", orderID)
			return nil
		}
		log.Printf("ERROR: Failed to push to orders collection: %v", err)
		return err
	}
	return nil
}

// RemoveActiveOrder deletes an order from Firestore when it's completed.
// This clears the ticket from POS and KDS screens.
func (c *Client) RemoveActiveOrder(ctx context.Context, orderID string) error {
	if c == nil || c.Firestore == nil {
		return nil
	}

	err := c.cb.Execute(func() error {
		_, e := c.Firestore.Collection("active_orders").Doc(orderID).Delete(ctx)
		return e
	})

	if err != nil {
		if err == circuitbreaker.ErrCircuitOpen {
			log.Printf("WARNING: Circuit breaker open, skipped RemoveActiveOrder for %s", orderID)
			return nil
		}
		log.Printf("ERROR: Failed to remove order %s from Firestore: %v", orderID, err)
		return err
	}

	log.Printf("Order %s removed from Firestore active_orders (cleared from screens)", orderID)
	return nil
}

// UpdateOrderStatus updates the status field in Firestore for real-time tracking.
// Customers watching their order status page will see the change instantly.
func (c *Client) UpdateOrderStatus(ctx context.Context, orderID string, status string) error {
	if c == nil || c.Firestore == nil {
		return nil
	}

	err := c.cb.Execute(func() error {
		_, e := c.Firestore.Collection("active_orders").Doc(orderID).Set(ctx, map[string]interface{}{
			"status": status,
		}, firestore.MergeAll)
		return e
	})

	if err != nil && err == circuitbreaker.ErrCircuitOpen {
		log.Printf("WARNING: Circuit breaker open, skipped UpdateOrderStatus for %s", orderID)
		return nil
	}

	return err
}

// UpdateOrderPayment sets the isPaid flag and payment method on an active order.
// This makes the "PAID" badge appear instantly on KDS/POS screens.
func (c *Client) UpdateOrderPayment(ctx context.Context, orderID string, isPaid bool, method string) error {
	if c == nil || c.Firestore == nil {
		return nil
	}

	err := c.cb.Execute(func() error {
		_, e := c.Firestore.Collection("active_orders").Doc(orderID).Set(ctx, map[string]interface{}{
			"isPaid":         isPaid,
			"payment_method": method,
		}, firestore.MergeAll)
		return e
	})

	if err != nil && err == circuitbreaker.ErrCircuitOpen {
		log.Printf("WARNING: Circuit breaker open, skipped UpdateOrderPayment for %s", orderID)
		return nil
	}

	return err
}

// VerifyAuthToken validates a Firebase ID token from the frontend.
// Returns the UID if valid, or an error if the token is forged/expired.
// We DO NOT wrap this in the circuit breaker, because auth must fail fast if unreachable.
func (c *Client) VerifyAuthToken(ctx context.Context, idToken string) (*auth.Token, error) {
	if c == nil || c.Auth == nil {
		return nil, nil
	}
	return c.Auth.VerifyIDToken(ctx, idToken)
}

// SendOrderReadyPush sends a push notification via FCM to the customer
// when their order is marked as ready.
func (c *Client) SendOrderReadyPush(ctx context.Context, fcmToken string, orderID string) error {
	if c == nil || c.Messaging == nil {
		log.Println("Messaging client not initialized, skipping push notification.")
		return nil
	}

	if fcmToken == "" {
		return nil // No token provided
	}

	msg := &messaging.Message{
		Token: fcmToken,
		Notification: &messaging.Notification{
			Title: "Your Order is Ready! 🎉",
			Body:  "Your delicious order is ready for collection at the counter.",
		},
		Data: map[string]string{
			"orderId": orderID,
			"url":     "/track/" + orderID,
		},
		Webpush: &messaging.WebpushConfig{
			Notification: &messaging.WebpushNotification{
				Icon: "/icon-192x192.png",
				Badge: "/icon-192x192.png",
				RequireInteraction: true,
			},
			FCMOptions: &messaging.WebpushFCMOptions{
				Link: "/track/" + orderID,
			},
		},
	}

	// Not using circuit breaker for this non-critical notification
	response, err := c.Messaging.Send(ctx, msg)
	if err != nil {
		log.Printf("ERROR: Failed to send FCM push for order %s: %v", orderID, err)
		return err
	}

	log.Printf("Successfully sent push notification to %s, response: %s", fcmToken, response)
	return nil
}
