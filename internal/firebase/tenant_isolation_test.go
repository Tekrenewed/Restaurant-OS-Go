package firebase

import (
	"os"
	"testing"
)

func TestDataIsolationMultiTenantMapping(t *testing.T) {
	// Configure simulated multi-tenant environment variables
	os.Setenv("TENANT_CONFIGS", "roti-naan-wala||uuid-roti-123|Roti Naan Wala,falooda-and-co||uuid-falooda-456|Falooda And Co")
	defer os.Unsetenv("TENANT_CONFIGS")

	// Instantiate the manager which parses the isolated configurations
	manager := NewTenantManager()

	// 1. Verify Falooda & Co data boundary
	faloodaClient := manager.GetClientByStoreID("uuid-falooda-456")
	if faloodaClient == nil {
		t.Fatalf("Expected strictly isolated client for Falooda but got nil")
	}
	if faloodaClient.Config.ProjectID != "falooda-and-co" {
		t.Errorf("Data Breach: Expected Falooda configurations mapped to Falooda store, got %s", faloodaClient.Config.ProjectID)
	}

	// 2. Verify Roti Naan Wala data boundary
	rotiClient := manager.GetClientByStoreID("uuid-roti-123")
	if rotiClient == nil {
		t.Fatalf("Expected strictly isolated client for Roti Naan Wala but got nil")
	}
	if rotiClient.Config.ProjectID != "roti-naan-wala" {
		t.Errorf("Data Breach: Expected Roti configurations mapped to Roti store, got %s", rotiClient.Config.ProjectID)
	}

	// 3. Verify that the two clients correctly hold separated configurations
	if faloodaClient.Config.ProjectID == rotiClient.Config.ProjectID {
		t.Errorf("FATAL Data Isolation Breach: Both store payloads map to the identical configuration space!")
	}
}
