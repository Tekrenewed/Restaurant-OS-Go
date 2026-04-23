package aggregator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"restaurant-os/internal/models"
	"restaurant-os/pkg/database"
)

// Platform defines the source of the delivery order
type Platform string

const (
	UberEats Platform = "UBER_EATS"
	JustEat  Platform = "JUST_EAT"
	Deliveroo Platform = "DELIVEROO"
)

// NormalizeUberEats takes a raw Uber Eats JSON payload and maps it to our InternalOrder, translating menu IDs
func NormalizeUberEats(ctx context.Context, db *database.DBWrapper, rawPayload []byte, tenantID string) (*models.InternalOrder, error) {
	// Uber Eats specific payload structure
	type uberOrder struct {
		ID    string `json:"id"`
		Store string `json:"store_id"`
		Cart  struct {
			Items []struct {
				ID       string  `json:"id"`
				Name     string  `json:"name"`
				Quantity int     `json:"quantity"`
				Price    float64 `json:"price"`
			} `json:"items"`
		} `json:"cart"`
		Payment struct {
			Total float64 `json:"total"`
		} `json:"payment"`
	}

	var uOrder uberOrder
	if err := json.Unmarshal(rawPayload, &uOrder); err != nil {
		return nil, fmt.Errorf("failed to parse uber eats payload: %w", err)
	}

	// Normalize into the real InternalOrder struct (see internal/models/order.go)
	order := &models.InternalOrder{
		ExternalID: uOrder.ID,               // Uber Eats order ID for idempotency
		Source:     "UberEats",               // Maps to db:"order_source"
		Status:     "PENDING",
		GrossTotal: uOrder.Payment.Total,
		CreatedAt:  time.Now(),
	}

	for _, item := range uOrder.Cart.Items {
		var internalProductID uuid.UUID
		
		// Attempt to map the Uber Eats item ID to our internal product ID
		query := `
			SELECT internal_product_id 
			FROM aggregator_menu_mappings 
			WHERE tenant_id = $1 AND platform = $2 AND external_item_id = $3
		`
		err := db.QueryRowContext(ctx, query, tenantID, string(UberEats), item.ID).Scan(&internalProductID)
		if err != nil {
			// If no mapping exists, log the error but we might still accept the order or reject it.
			// For now, we will fail the normalization if an item isn't mapped, so we don't cook the wrong thing.
			return nil, fmt.Errorf("unmapped Uber Eats item: ID '%s', Name '%s'. Please map this item in the POS dashboard", item.ID, item.Name)
		}

		order.Items = append(order.Items, models.OrderItem{
			ProductID: internalProductID,
			Name:      item.Name,
			PricePaid: item.Price * float64(item.Quantity),
		})
	}

	return order, nil
}
