package database

import (
	"context"
	"database/sql"
	"errors"
)

var ErrOrderAlreadyExists = errors.New("order already exists (idempotency triggered)")

// CheckIdempotency checks if an external order ID from a specific aggregator already exists in the database.
func CheckIdempotency(ctx context.Context, db *DBWrapper, tenantID string, externalOrderID string, platform string) error {
	// In a real application, this checks a uniqueness constraint or specific external_orders table.
	// For now, we simulate the check.
	if db == nil {
		return nil // DB not connected
	}

	query := `SELECT id FROM aggregator_orders WHERE tenant_id = $1 AND external_order_id = $2 AND platform = $3 LIMIT 1`
	var id string
	err := db.QueryRowContext(ctx, query, tenantID, externalOrderID, platform).Scan(&id)
	
	if err == sql.ErrNoRows {
		return nil // Safe to proceed, order is new
	} else if err != nil {
		return err // Real DB error
	}

	// Order was found
	return ErrOrderAlreadyExists
}
