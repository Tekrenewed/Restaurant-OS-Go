package events

import (
	"context"
	"log"
)

// ─── NoopPublisher ───
// A no-operation publisher used for:
//   - Unit testing (no side effects)
//   - Running the Go server without any real-time backend configured
//   - Dry-run migrations
//
// Every method logs the event but does nothing else.
type NoopPublisher struct{}

func NewNoopPublisher() *NoopPublisher {
	return &NoopPublisher{}
}

func (n *NoopPublisher) PublishOrderCreated(ctx context.Context, order OrderEvent) error {
	log.Printf("[EventBus/Noop] Order created: %s (status=%s)", order.ID, order.Status)
	return nil
}

func (n *NoopPublisher) PublishOrderUpdated(ctx context.Context, orderID string, fields map[string]interface{}) error {
	log.Printf("[EventBus/Noop] Order updated: %s", orderID)
	return nil
}

func (n *NoopPublisher) PublishBookingCreated(ctx context.Context, booking BookingEvent) error {
	log.Printf("[EventBus/Noop] Booking created: %s", booking.ID)
	return nil
}

func (n *NoopPublisher) PublishBookingUpdated(ctx context.Context, bookingID string, fields map[string]interface{}) error {
	log.Printf("[EventBus/Noop] Booking updated: %s", bookingID)
	return nil
}

func (n *NoopPublisher) PublishMenuItemUpdated(ctx context.Context, itemID string, fields map[string]interface{}) error {
	log.Printf("[EventBus/Noop] Menu item updated: %s", itemID)
	return nil
}

func (n *NoopPublisher) PublishSoldOutChanged(ctx context.Context, soldOutItems []string) error {
	log.Printf("[EventBus/Noop] Sold-out list changed: %d items", len(soldOutItems))
	return nil
}
