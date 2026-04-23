package models

import (
	"github.com/google/uuid"
)

// SpiceLevel indicates the spiciness of a savory dish
type SpiceLevel string

const (
	SpiceLevelMild   SpiceLevel = "MILD"
	SpiceLevelMedium SpiceLevel = "MEDIUM"
	SpiceLevelHot    SpiceLevel = "HOT"
	SpiceLevelExtraHot SpiceLevel = "EXTRA_HOT"
)

// DietaryFlag indicates dietary preferences or restrictions
type DietaryFlag string

const (
	DietaryVegan       DietaryFlag = "VEGAN"
	DietaryVegetarian  DietaryFlag = "VEGETARIAN"
	DietaryGlutenFree  DietaryFlag = "GLUTEN_FREE"
	DietaryDairyFree   DietaryFlag = "DAIRY_FREE"
	DietaryContainsNuts DietaryFlag = "CONTAINS_NUTS"
)

// MenuCategory groups items (e.g. "Curry", "Grill", "Starters", "Desserts", "Drinks")
type MenuCategory struct {
	ID          uuid.UUID `json:"id" db:"id"`
	StoreID     uuid.UUID `json:"store_id" db:"store_id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	SortOrder   int       `json:"sort_order" db:"sort_order"`
}

// MenuItem represents a product on the Taste of Village menu
type MenuItem struct {
	ID           uuid.UUID     `json:"id" db:"id"`
	StoreID      uuid.UUID     `json:"store_id" db:"store_id"`
	CategoryID   uuid.UUID     `json:"category_id" db:"category_id"`
	Name         string        `json:"name" db:"name"`
	Description  string        `json:"description" db:"description"`
	Price        float64       `json:"price" db:"price"`
	ImageURL     string        `json:"image_url" db:"image_url"`
	IsAvailable  bool          `json:"is_available" db:"is_available"`
	SpiceLevel   *SpiceLevel   `json:"spice_level" db:"spice_level"`     // Optional, mainly for curries/grills
	DietaryFlags []DietaryFlag `json:"dietary_flags" db:"-"`               // E.g., [VEGAN, GLUTEN_FREE]
	Allergens    []string      `json:"allergens" db:"-"`                   // E.g., ["Peanuts", "Dairy"]
}
