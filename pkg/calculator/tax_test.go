package calculator

import (
	"restaurant-os/internal/models"
	"testing"
)

func TestCalculateFinance(t *testing.T) {
	items := []models.OrderItem{
		{Name: "Chicken Tikka Masala", PricePaid: 9.49, IsZeroRated: false}, // 20% VAT
		{Name: "Coke Can", PricePaid: 1.50, IsZeroRated: false},             // 20% VAT
		{Name: "Cold Takeaway Snack", PricePaid: 3.00, IsZeroRated: true},   // 0% VAT
	}

	// 1. Without Service Charge
	summaryNoService := CalculateFinance(items, false)
	
	expectedGross := 13.99 // 9.49 + 1.50 + 3.00
	if summaryNoService.GrossTotal != expectedGross {
		t.Errorf("Expected GrossTotal %f, got %f", expectedGross, summaryNoService.GrossTotal)
	}

	// expectedNet = (10.99 / 1.20) + 3.00 // 9.1583... + 3.00 = 12.16
	expectedNetRounded := 12.16          // mathRound applied
	if summaryNoService.NetTotal != expectedNetRounded {
		t.Errorf("Expected NetTotal %f, got %f", expectedNetRounded, summaryNoService.NetTotal)
	}

	// 2. With Service Charge (12.5% on 13.99)
	summaryService := CalculateFinance(items, true)
	expectedService := 1.75 // 13.99 * 0.125 = 1.74875 -> 1.75 
	
	if summaryService.ServiceCharge != expectedService {
		t.Errorf("Expected ServiceCharge %f, got %f", expectedService, summaryService.ServiceCharge)
	}
	
	expectedGrossWithService := expectedGross + expectedService
	if summaryService.GrossTotal != expectedGrossWithService {
		t.Errorf("Expected GrossTotal %f, got %f", expectedGrossWithService, summaryService.GrossTotal)
	}
}
