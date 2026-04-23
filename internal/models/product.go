package models

import (
	"github.com/google/uuid"
)

// Product represents a menu item
type Product struct {
	ID          uuid.UUID `json:"id" db:"id"`
	BrandID     uuid.UUID `json:"brand_id" db:"brand_id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	Category    string    `json:"category" db:"category"`
	Is86d       bool      `json:"is_86d" db:"is_86d"`
}


// Catalog is the full menu returned to the frontend
type Catalog struct {
	Products []Product `json:"products"`
}
