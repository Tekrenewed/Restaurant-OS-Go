package calculator

import (
	"math"
	"restaurant-os/internal/models"
)

// CalculateFinance processes the totals for a UK-based order
// Handles 20% Standard Rate, 0% Zero-Rate, and an optional 12.5% Service Charge
func CalculateFinance(items []models.OrderItem, applyServiceCharge bool) models.OrderSummary {
	var summary models.OrderSummary
	const serviceRate = 0.125 // Standard 12.5% UK Service Charge

	for i := range items {
		vatRate := 0.20
		if items[i].IsZeroRated {
			vatRate = 0.00
		}

		// Update the item's VAT rate for record keeping
		items[i].VATRate = vatRate

		// UK VAT is inclusive. Reverse calculate Net from Gross: Net = Gross / (1 + Rate)
		itemNet := items[i].PricePaid / (1 + vatRate)
		itemVAT := items[i].PricePaid - itemNet

		summary.NetTotal += itemNet
		summary.VATTotal += itemVAT
		summary.GrossTotal += items[i].PricePaid
	}

	if applyServiceCharge {
		// Calculate service charge on the subtotal before applying service charge itself
		summary.ServiceCharge = mathRound(summary.GrossTotal * serviceRate)
		summary.GrossTotal += summary.ServiceCharge
	}

	// Double check rounding for all totals to 2 decimal places
	summary.NetTotal = mathRound(summary.NetTotal)
	summary.VATTotal = mathRound(summary.VATTotal)
	summary.GrossTotal = mathRound(summary.GrossTotal)

	return summary
}

// mathRound helper rounds to 2 decimal places for financial safety
func mathRound(val float64) float64 {
	return math.Round(val*100) / 100
}
