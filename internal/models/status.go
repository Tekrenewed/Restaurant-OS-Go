package models

// ─── Order Status Constants ───
// These are the SINGLE SOURCE OF TRUTH for all order statuses.
// Every handler, service, and Firestore write MUST use these constants.
// Never use raw strings like "pending" — always use StatusPending.
const (
	StatusWebHolding = "web_holding" // Web order received, awaiting staff action
	StatusPending    = "pending"     // Sent to kitchen (in KDS queue)
	StatusPreparing  = "preparing"   // Kitchen is actively making this order
	StatusReady      = "ready"       // Order is ready for collection/delivery
	StatusCompleted  = "completed"   // Order collected/delivered — done
	StatusNoShow     = "no_show"     // Customer didn't show up — record kept
)

// ValidStatuses is used for runtime validation at API boundaries.
var ValidStatuses = map[string]bool{
	StatusWebHolding: true,
	StatusPending:    true,
	StatusPreparing:  true,
	StatusReady:      true,
	StatusCompleted:  true,
	StatusNoShow:     true,
}

// IsValidStatus checks if a status string is in the allowed set.
func IsValidStatus(s string) bool {
	return ValidStatuses[s]
}

// ─── Order Source Constants ───
const (
	SourcePOS      = "POS"
	SourceWeb      = "Web"
	SourceUberEats = "UberEats"
	SourceDeliveroo = "Deliveroo"
	SourceJustEat  = "JustEat"
)
