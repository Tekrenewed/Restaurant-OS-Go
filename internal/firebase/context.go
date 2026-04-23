package firebase

import (
	"context"

	"cloud.google.com/go/firestore"
	"firebase.google.com/go/v4/auth"
)

// contextKey is an unexported type to prevent collisions with other packages
type contextKey string

const (
	tenantClientKey contextKey = "tenant_client"
	tenantTokenKey  contextKey = "tenant_token"
)

// TenantContext holds the resolved tenant information for the current request.
// This is injected into the request context by the TenantAuthMiddleware.
type TenantContext struct {
	Client    *TenantClient // The Firebase client for this restaurant
	Token     *auth.Token   // The decoded Firebase Auth JWT
	StoreID   string        // The internal PostgreSQL store UUID
	ProjectID string        // The Firebase project ID
}

// WithTenantContext stores TenantContext in the request context
func WithTenantContext(ctx context.Context, tc *TenantContext) context.Context {
	return context.WithValue(ctx, tenantClientKey, tc)
}

// GetTenantContext retrieves TenantContext from the request context.
// Returns nil if no tenant context is set (e.g., unauthenticated or single-tenant mode).
func GetTenantContext(ctx context.Context) *TenantContext {
	v, _ := ctx.Value(tenantClientKey).(*TenantContext)
	return v
}

// GetTenantFirestore is a convenience method that returns the Firestore client
// for the current request's tenant. Returns nil if no tenant context is set.
func GetTenantFirestore(ctx context.Context) *firestore.Client {
	tc := GetTenantContext(ctx)
	if tc == nil || tc.Client == nil {
		return nil
	}
	return tc.Client.Firestore
}

// GetTenantStoreID returns the PostgreSQL store_id for the current request's tenant.
// Returns empty string if no tenant context is set.
func GetTenantStoreID(ctx context.Context) string {
	tc := GetTenantContext(ctx)
	if tc == nil {
		return ""
	}
	return tc.StoreID
}
