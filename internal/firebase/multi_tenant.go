package firebase

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

// TenantConfig holds the configuration for a single restaurant tenant
type TenantConfig struct {
	ProjectID   string // Firebase project ID, e.g., "roti-naan-wala"
	CredFile    string // Path to service account key (empty = use ADC)
	StoreID     string // Internal PostgreSQL store UUID
	DisplayName string // Human-friendly name, e.g., "Roti Naan Wala"
}

// TenantClient holds Firebase services for one tenant
type TenantClient struct {
	Auth      *auth.Client
	Firestore *firestore.Client
	Config    TenantConfig
}

// TenantManager manages multiple Firebase project connections.
// Each restaurant gets its own Firebase project, Auth, and Firestore.
type TenantManager struct {
	mu      sync.RWMutex
	clients map[string]*TenantClient // keyed by Firebase project ID
	// defaultProjectID is the primary/fallback tenant (Falooda & Co)
	defaultProjectID string
}

// NewTenantManager creates a TenantManager and initialises all configured tenants.
//
// Configuration is read from environment variables:
//
//	TENANT_CONFIGS = "falooda-and-co|/path/to/key.json|f4100da2-...|Falooda & Co,roti-naan-wala|/path/to/key2.json|abc123-...|Roti Naan Wala"
//
// Or for Cloud Run (ADC), credentials paths can be empty:
//
//	TENANT_CONFIGS = "falooda-and-co||f4100da2-...|Falooda & Co,roti-naan-wala||abc123-...|Roti Naan Wala"
//
// The first tenant listed becomes the default (fallback).
func NewTenantManager() *TenantManager {
	tm := &TenantManager{
		clients: make(map[string]*TenantClient),
	}

	configStr := os.Getenv("TENANT_CONFIGS")
	if configStr == "" {
		log.Println("TENANT_CONFIGS not set — falling back to single-tenant mode")
		return tm
	}

	tenants := strings.Split(configStr, ",")
	for i, raw := range tenants {
		parts := strings.Split(strings.TrimSpace(raw), "|")
		if len(parts) < 4 {
			log.Printf("WARNING: Skipping malformed tenant config: %q (expected projectID|credFile|storeID|displayName)", raw)
			continue
		}

		tc := TenantConfig{
			ProjectID:   strings.TrimSpace(parts[0]),
			CredFile:    strings.TrimSpace(parts[1]),
			StoreID:     strings.TrimSpace(parts[2]),
			DisplayName: strings.TrimSpace(parts[3]),
		}

		client, err := tm.initTenant(tc)
		if err != nil {
			log.Printf("WARNING: Failed to initialise tenant %q: %v", tc.ProjectID, err)
			continue
		}

		tm.mu.Lock()
		tm.clients[tc.ProjectID] = client
		if i == 0 {
			tm.defaultProjectID = tc.ProjectID
		}
		tm.mu.Unlock()

		log.Printf("Tenant initialised: %s (%s) → store %s", tc.DisplayName, tc.ProjectID, tc.StoreID)
	}

	log.Printf("TenantManager ready: %d tenants loaded, default=%s", len(tm.clients), tm.defaultProjectID)
	return tm
}

// initTenant creates Firebase App + Auth + Firestore for a single tenant
func (tm *TenantManager) initTenant(tc TenantConfig) (*TenantClient, error) {
	ctx := context.Background()

	config := &firebase.Config{
		ProjectID: tc.ProjectID,
	}

	var app *firebase.App
	var err error

	if tc.CredFile != "" {
		opt := option.WithCredentialsFile(tc.CredFile)
		app, err = firebase.NewApp(ctx, config, opt)
	} else {
		// Cloud Run ADC — still need to specify project ID for multi-project
		app, err = firebase.NewApp(ctx, config)
	}
	if err != nil {
		return nil, fmt.Errorf("firebase.NewApp: %w", err)
	}

	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("app.Auth: %w", err)
	}

	fsClient, err := app.Firestore(ctx)
	if err != nil {
		return nil, fmt.Errorf("app.Firestore: %w", err)
	}

	return &TenantClient{
		Auth:      authClient,
		Firestore: fsClient,
		Config:    tc,
	}, nil
}

// GetClient returns the TenantClient for the given Firebase project ID.
// Returns nil if no tenant is found.
func (tm *TenantManager) GetClient(projectID string) *TenantClient {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.clients[projectID]
}

// GetClientByStoreID returns the TenantClient for the given internal store UUID.
func (tm *TenantManager) GetClientByStoreID(storeID string) *TenantClient {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	for _, tc := range tm.clients {
		if tc.Config.StoreID == storeID {
			return tc
		}
	}
	return nil
}

// GetDefault returns the default (primary) tenant client.
func (tm *TenantManager) GetDefault() *TenantClient {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.clients[tm.defaultProjectID]
}

// DefaultProjectID returns the project ID of the default tenant.
func (tm *TenantManager) DefaultProjectID() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.defaultProjectID
}

// AllClients returns all registered tenant clients.
func (tm *TenantManager) AllClients() map[string]*TenantClient {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	// Return a copy to prevent mutation
	result := make(map[string]*TenantClient, len(tm.clients))
	for k, v := range tm.clients {
		result[k] = v
	}
	return result
}

// VerifyTokenAndResolveTenant takes a raw Firebase ID token, tries to verify it
// against ALL registered tenant Auth clients, and returns the matched tenant + decoded token.
// This is the core of multi-tenant JWT resolution.
func (tm *TenantManager) VerifyTokenAndResolveTenant(ctx context.Context, idToken string) (*TenantClient, *auth.Token, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// First, try the default tenant (most common case, skip unnecessary iterations)
	if def, ok := tm.clients[tm.defaultProjectID]; ok && def.Auth != nil {
		token, err := def.Auth.VerifyIDToken(ctx, idToken)
		if err == nil {
			return def, token, nil
		}
	}

	// Fall through to all other tenants
	for pid, tc := range tm.clients {
		if pid == tm.defaultProjectID {
			continue // Already tried
		}
		if tc.Auth == nil {
			continue
		}
		token, err := tc.Auth.VerifyIDToken(ctx, idToken)
		if err == nil {
			return tc, token, nil
		}
	}

	return nil, nil, fmt.Errorf("token could not be verified against any registered tenant")
}
