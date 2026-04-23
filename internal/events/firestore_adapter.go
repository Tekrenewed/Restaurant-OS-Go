package events

import (
	"context"
	"fmt"
	"log"

	"cloud.google.com/go/firestore"
)

// ─── FirestorePublisher ───
// The Firestore adapter for the RealtimePublisher interface.
// This is the "plug" that connects to the Firestore "power source".
//
// When we're ready to switch to Redis, we create a RedisPublisher
// with the exact same method signatures — no handler code changes.
type FirestorePublisher struct {
	// GetClient returns the correct Firestore client for a given store/tenant.
	// In multi-tenant mode, this resolves the right project.
	// In single-tenant mode, it always returns the default client.
	GetClient func(storeID string) *firestore.Client
}

// NewFirestorePublisher creates a publisher backed by Firestore.
func NewFirestorePublisher(getClient func(storeID string) *firestore.Client) *FirestorePublisher {
	return &FirestorePublisher{GetClient: getClient}
}

// ─── Order Events ───

func (fp *FirestorePublisher) PublishOrderCreated(ctx context.Context, order OrderEvent) error {
	client := fp.GetClient(order.StoreID)
	if client == nil {
		return fmt.Errorf("no Firestore client for store %s", order.StoreID)
	}

	// Build the Firestore document shape that the React frontend expects
	doc := map[string]interface{}{
		"id":            order.ID,
		"customerName":  order.CustomerName,
		"customerPhone": order.CustomerPhone,
		"type":          order.OrderType,
		"table_number":  order.TableNumber,
		"total":         order.Total,
		"status":        order.Status,
		"source":        order.Source,
		"tenantId":      order.TenantID,
		"timestamp":     order.Timestamp,
	}

	// Build items array matching CartItem shape
	items := make([]map[string]interface{}, len(order.Items))
	for i, item := range order.Items {
		items[i] = map[string]interface{}{
			"name":     item.Name,
			"price":    item.Price,
			"quantity": item.Quantity,
			"image":    item.Image,
		}
		if items[i]["image"] == "" {
			items[i]["image"] = "/assets/placeholder.jpg"
		}
	}
	doc["items"] = items

	// Merge any extra metadata
	for k, v := range order.Extra {
		doc[k] = v
	}

	// Use ExternalID as doc ID for web orders, otherwise use the primary ID
	docID := order.ID
	if order.Source == "Web" && order.ExternalID != "" {
		docID = order.ExternalID
		doc["id"] = order.ExternalID
	}

	_, err := client.Collection("orders").Doc(docID).Set(ctx, doc)
	if err != nil {
		log.Printf("[EventBus/Firestore] Failed to publish order %s: %v", docID, err)
		return err
	}

	log.Printf("[EventBus/Firestore] Order published: %s (store=%s)", docID, order.StoreID)
	return nil
}

func (fp *FirestorePublisher) PublishOrderUpdated(ctx context.Context, orderID string, fields map[string]interface{}) error {
	// For updates, we need to know which store. Check if storeID is in the fields.
	storeID, _ := fields["_storeID"].(string)
	delete(fields, "_storeID") // Don't persist the routing key

	client := fp.GetClient(storeID)
	if client == nil {
		// Fallback to default
		client = fp.GetClient("")
	}
	if client == nil {
		return fmt.Errorf("no Firestore client available for order update %s", orderID)
	}

	_, err := client.Collection("orders").Doc(orderID).Set(ctx, fields, firestore.MergeAll)
	if err != nil {
		log.Printf("[EventBus/Firestore] Failed to update order %s: %v", orderID, err)
	}
	return err
}

// ─── Booking Events ───

func (fp *FirestorePublisher) PublishBookingCreated(ctx context.Context, booking BookingEvent) error {
	client := fp.GetClient("") // Bookings use default tenant for now
	if client == nil {
		return fmt.Errorf("no Firestore client for bookings")
	}

	doc := map[string]interface{}{
		"id":            booking.ID,
		"customerName":  booking.CustomerName,
		"customerPhone": booking.CustomerPhone,
		"email":         booking.Email,
		"date":          booking.Date,
		"time":          booking.Time,
		"guests":        booking.Guests,
		"status":        booking.Status,
	}

	_, err := client.Collection("bookings").Doc(booking.ID).Set(ctx, doc)
	if err != nil {
		log.Printf("[EventBus/Firestore] Failed to publish booking %s: %v", booking.ID, err)
	}
	return err
}

func (fp *FirestorePublisher) PublishBookingUpdated(ctx context.Context, bookingID string, fields map[string]interface{}) error {
	client := fp.GetClient("")
	if client == nil {
		return fmt.Errorf("no Firestore client for booking update")
	}

	_, err := client.Collection("bookings").Doc(bookingID).Set(ctx, fields, firestore.MergeAll)
	if err != nil {
		log.Printf("[EventBus/Firestore] Failed to update booking %s: %v", bookingID, err)
	}
	return err
}

// ─── Menu Events ───

func (fp *FirestorePublisher) PublishMenuItemUpdated(ctx context.Context, itemID string, fields map[string]interface{}) error {
	client := fp.GetClient("")
	if client == nil {
		return fmt.Errorf("no Firestore client for menu update")
	}

	_, err := client.Collection("menu_items").Doc(itemID).Set(ctx, fields, firestore.MergeAll)
	if err != nil {
		log.Printf("[EventBus/Firestore] Failed to update menu item %s: %v", itemID, err)
	}
	return err
}

func (fp *FirestorePublisher) PublishSoldOutChanged(ctx context.Context, soldOutItems []string) error {
	client := fp.GetClient("")
	if client == nil {
		return fmt.Errorf("no Firestore client for sold-out update")
	}

	_, err := client.Collection("settings").Doc("sold_out").Set(ctx, map[string]interface{}{
		"items": soldOutItems,
	})
	if err != nil {
		log.Printf("[EventBus/Firestore] Failed to update sold-out list: %v", err)
	}
	return err
}
