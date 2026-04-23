package api

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"restaurant-os/internal/events"
	"restaurant-os/internal/firebase"
	"restaurant-os/internal/models"
	"restaurant-os/pkg/calculator"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// Server encapsulates all dependencies required by the HTTP handlers
type Server struct {
	DB        *sqlx.DB
	Hub       *Hub
	CfdHub    *CfdHub
	Firebase  *firebase.Client         // Default tenant (Falooda & Co) — backwards compat
	Tenants   *firebase.TenantManager  // Multi-tenant Firebase manager (nil = single-tenant mode)
	Publisher events.RealtimePublisher // Decoupled event bus — today Firestore, tomorrow Redis
}

// GetFirestoreForRequest returns the correct Firestore client for the current request.
// In multi-tenant mode, checks the request context first.
// Falls back to the default Firebase client.
func (s *Server) GetFirestoreForRequest(r *http.Request) *firebase.TenantClient {
	// 1. Check if tenant was resolved from JWT by the auth middleware
	if tc := firebase.GetTenantContext(r.Context()); tc != nil {
		return tc.Client
	}

	// 2. If no JWT, try resolving via X-Store-ID header (for PIN login / public catalog)
	if s.Tenants != nil {
		storeID := r.Header.Get("X-Store-ID")
		if storeID != "" {
			if client := s.Tenants.GetClientByStoreID(storeID); client != nil {
				return client
			}
		}
		// 3. Fall back to default (Falooda & Co)
		return s.Tenants.GetDefault()
	}
	return nil
}

// HandleCreateOrder processes standard POS, Web, or Kiosk orders
func (s *Server) HandleCreateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var order models.InternalOrder
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		log.Printf("Order decode error: %v", err)
		http.Error(w, `{"error":"invalid_payload","message":"Invalid order format"}`, http.StatusBadRequest)
		return
	}

	// 1. Calculate Totals using UK Tax Logic
	summary := calculator.CalculateFinance(order.Items, order.ApplyServiceCharge)

	// Update the order with calculated sums
	// Preserve the ORD- prefix from the Web frontend
	if order.Source == "Web" && order.ExternalID != "" {
		order.ID = uuid.New() // We still need a valid UUID for PosgreSQL PK
	} else {
		order.ID = uuid.New()
	}
	
	tc := s.GetFirestoreForRequest(r)
	
	// Force the store ID if none provided or it's a zero UUID, to prevent FK failures
	if order.StoreID == uuid.Nil {
		if tc != nil && tc.Config.StoreID != "" {
			parsed, err := uuid.Parse(tc.Config.StoreID)
			if err == nil {
				order.StoreID = parsed
			}
		}
		
		// Ultimate fallback if no tenant config found
		if order.StoreID == uuid.Nil {
			order.StoreID = uuid.MustParse("f4100da2-1111-1111-1111-000000000001") // Falooda & Co Main Store
		}
	}
	
	order.NetTotal = summary.NetTotal
	order.VATTotal = summary.VATTotal
	order.ServiceCharge = summary.ServiceCharge
	order.GrossTotal = summary.GrossTotal
	// Web orders start as "web_holding" so staff see them in the Waiting queue first.
	// POS orders go straight to "pending" (they're already physically in-store).
	if order.Source == models.SourceWeb {
		order.Status = models.StatusWebHolding
	} else {
		order.Status = models.StatusPending
	}
	order.NeedsPrinting = true
	order.CreatedAt = time.Now()

	// --- 1.5. The "1-Click" Online Checkout Trap & POS Capture ---
	// Silently capture the customer into the CRM if they provided a phone or email.
	// Writes to BOTH PostgreSQL and Firestore simultaneously (data split fix).
	var cPhone, cEmail, cName string
	var customerID string // Postgres UUID for relational linking
	if order.CustomerPhone != nil {
		cPhone = normalisePhone(*order.CustomerPhone)
	}
	if order.CustomerEmail != nil {
		cEmail = *order.CustomerEmail
	}
	if order.CustomerName != nil {
		cName = *order.CustomerName
	}
	
	storeIDStr := ""
	var fsClient *firestore.Client
	if tc != nil {
		storeIDStr = tc.Config.StoreID
		fsClient = tc.Firestore
	}

	if (cPhone != "" || cEmail != "") && s.DB != nil {
		// 1. Silently Create Profile in BOTH Postgres + Firestore
		customerID = UpsertCustomerInternal(s.DB, fsClient, r.Context(), storeIDStr, cPhone, cEmail, cName)
		
		// 2. Auto-award loyalty points for this order (10 points per checkout to hook them)
		if fsClient != nil && cPhone != "" {
			_ = s.AddLoyaltyPointsInternal(r.Context(), fsClient, storeIDStr, cPhone, 10)
		}
	}
	// -------------------------------------------------------------

	// 2. Save Order to PostgreSQL (permanent record — source of truth)
	if s.DB != nil {
		tx, err := s.DB.Beginx()
		if err != nil {
			log.Printf("Database error on begin tx: %v", err)
			http.Error(w, `{"error":"server_error","message":"Could not process order"}`, http.StatusInternalServerError)
			return
		}

		// Link customer_id to order if we captured one
		if customerID != "" {
			order.CustomerID = &customerID
		}

		// Insert Main Order Record
		insertOrderQuery := `
			INSERT INTO orders (id, store_id, order_source, net_total, vat_total, service_charge, gross_total, status, needs_printing, created_at, table_number, customer_name, customer_phone, customer_id)
			VALUES (:id, :store_id, :order_source, :net_total, :vat_total, :service_charge, :gross_total, :status, :needs_printing, :created_at, :table_number, :customer_name, :customer_phone, :customer_id)
		`
		if _, err := tx.NamedExec(insertOrderQuery, &order); err != nil {
			log.Printf("Failed to insert main order: %v", err)
			tx.Rollback()
			http.Error(w, `{"error":"server_error","message":"Failed to save order"}`, http.StatusInternalServerError)
			return
		}

		// Insert Individual Items
		for _, item := range order.Items {
			item.ID = uuid.New()
			item.OrderID = order.ID
			// product_id is intentionally omitted from the insert to allow PostgreSQL to default it to NULL,
			// avoiding Foreign Key constraint violations when the frontend only provides product names.
			insertItemQuery := `
				INSERT INTO order_items (id, order_id, name, price_paid, is_takeaway, vat_rate)
				VALUES (:id, :order_id, :name, :price_paid, :is_takeaway, :vat_rate)
			`
			if _, err := tx.NamedExec(insertItemQuery, &item); err != nil {
				log.Printf("Failed to insert order item: %v", err)
				tx.Rollback()
				http.Error(w, `{"error":"server_error","message":"Failed to save order item"}`, http.StatusInternalServerError)
				return
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("Failed to commit transaction: %v", err)
			http.Error(w, `{"error":"server_error","message":"Transaction failed"}`, http.StatusInternalServerError)
			return
		}
	}

	// 3. Push to Firestore for real-time POS/KDS sync (the hybrid magic)
	if tc != nil && tc.Firestore != nil {
		orderType := "collection" // Default for web orders
		if order.TableNumber != nil && *order.TableNumber > 0 {
			orderType = "dine-in"
		}

		firestoreData := map[string]interface{}{
			"customerName":  order.CustomerName,
			"customerPhone": order.CustomerPhone,
			"type":          orderType,
			"table_number":  order.TableNumber,
			"total":         order.GrossTotal,
			"status":        order.Status,
			"source":        order.Source,
		}

		// Add items as a nested array matching the CartItem shape the POS renders
		itemsList := make([]map[string]interface{}, len(order.Items))
		for i, item := range order.Items {
			itemsList[i] = map[string]interface{}{
				"name":     item.Name,
				"price":    item.PricePaid,
				"quantity": 1,
				"image":    "/assets/placeholder.jpg",
			}
		}
		firestoreData["items"] = itemsList
		firestoreData["timestamp"] = order.CreatedAt

		// Use the external_id as the Firestore doc ID for Web orders (so the
		// frontend can track by ORD-xxx), otherwise use the Postgres UUID.
		docID := order.ID.String()
		if order.Source == "Web" && order.ExternalID != "" {
			docID = order.ExternalID
			firestoreData["id"] = order.ExternalID
		} else {
			firestoreData["id"] = docID
		}

		// ALL orders go to the "orders" collection — this is the single source
		// of truth that streamOrders() in the React frontend listens to.
		// Never split POS vs Web into different collections.
		_, err := tc.Firestore.Collection("orders").Doc(docID).Set(r.Context(), firestoreData)
		if err != nil {
			log.Printf("ERROR: Failed to push to orders collection on tenant %s: %v", tc.Config.ProjectID, err)
		}
	}

	// 4. Also broadcast via WebSocket (kept for backwards compat)
	s.Hub.Broadcast <- order

	// 5. Return Full Summary back to the Kiosk or POS client so they can print receipts
	summary.OrderID = order.ID.String()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(summary)
}

// HandleMigrate forcefully runs pending DDL statements
func (s *Server) HandleMigrate(w http.ResponseWriter, r *http.Request) {
	if s.DB == nil {
		http.Error(w, `{"error":"database_unavailable","message":"No database connection. Check DATABASE_URL."}`, http.StatusServiceUnavailable)
		return
	}

	// Step 0: Run the full schema.sql first to ensure all tables exist
	schemaBytes, err := os.ReadFile("migrations/schema.sql")
	if err == nil && len(schemaBytes) > 0 {
		if _, err := s.DB.Exec(string(schemaBytes)); err != nil {
			log.Printf("Schema creation failed: %v", err)
			// Don't return — tables might already exist, continue with ALTERs
		} else {
			log.Println("Schema.sql executed successfully")
		}
	}

	// Step 1: Run ALTER TABLE migrations for incremental schema changes
	queries := []string{
		"ALTER TABLE orders ADD COLUMN IF NOT EXISTS table_number INTEGER;",
		"ALTER TABLE orders ADD COLUMN IF NOT EXISTS customer_name VARCHAR(255);",
		"ALTER TABLE orders ADD COLUMN IF NOT EXISTS customer_phone VARCHAR(50);",
		"ALTER TABLE orders ADD COLUMN IF NOT EXISTS customer_email VARCHAR(255);",
		"ALTER TABLE order_items ADD COLUMN IF NOT EXISTS is_takeaway BOOLEAN DEFAULT false;",
		"ALTER TABLE products ADD COLUMN IF NOT EXISTS is_86d BOOLEAN DEFAULT false;",
		"ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_status VARCHAR(20) DEFAULT 'unpaid';",
		"ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_method VARCHAR(50);",
		"ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_reference VARCHAR(255);",
		"ALTER TABLE orders ADD COLUMN IF NOT EXISTS completed_at TIMESTAMP WITH TIME ZONE;",
		"ALTER TABLE orders ADD COLUMN IF NOT EXISTS is_printed BOOLEAN DEFAULT false;",
		"ALTER TABLE orders ADD COLUMN IF NOT EXISTS printed_at TIMESTAMP WITH TIME ZONE;",
		// Phase 4: Dojo Cloud Payment Integration
		"ALTER TABLE stores ADD COLUMN IF NOT EXISTS dojo_terminal_id VARCHAR(255);",
		"ALTER TABLE orders ADD COLUMN IF NOT EXISTS dojo_intent_id VARCHAR(255);",
		// ─── SaaS Transformation: Week 1 ───
		// Tenant isolation for customers
		"ALTER TABLE customers ADD COLUMN IF NOT EXISTS store_id UUID REFERENCES stores(id);",
		// Relational link: orders → customers (via phone, email, name)
		"ALTER TABLE orders ADD COLUMN IF NOT EXISTS customer_id UUID REFERENCES customers(id);",
		// RFM Customer Scoring
		`CREATE TABLE IF NOT EXISTS customer_scores (
			customer_id UUID PRIMARY KEY REFERENCES customers(id),
			store_id    UUID REFERENCES stores(id),
			recency_score   INT DEFAULT 0,
			frequency_score INT DEFAULT 0,
			monetary_score  INT DEFAULT 0,
			total_score     INT DEFAULT 0,
			segment         VARCHAR(20) DEFAULT 'NEW',
			updated_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);`,
		// Co-Purchase Recommendations
		`CREATE TABLE IF NOT EXISTS product_recommendations (
			product_id             UUID REFERENCES products(id),
			recommended_product_id UUID REFERENCES products(id),
			store_id               UUID REFERENCES stores(id),
			co_purchase_count      INT DEFAULT 0,
			PRIMARY KEY (product_id, recommended_product_id, store_id)
		);`,
		// Campaign Engine
		`CREATE TABLE IF NOT EXISTS campaigns (
			id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			store_id    UUID REFERENCES stores(id),
			name        VARCHAR(255) NOT NULL,
			segment     VARCHAR(20) NOT NULL,
			offer_type  VARCHAR(50) NOT NULL,
			offer_value DECIMAL(10,2),
			offer_code  VARCHAR(50),
			channel     VARCHAR(20) DEFAULT 'email',
			message_html TEXT,
			status      VARCHAR(20) DEFAULT 'draft',
			scheduled_at TIMESTAMP WITH TIME ZONE,
			sent_at     TIMESTAMP WITH TIME ZONE,
			recipients_count INT DEFAULT 0,
			created_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);`,
		// Bookings / Reservations
		`CREATE TABLE IF NOT EXISTS bookings (
			id              VARCHAR(255) PRIMARY KEY,
			customer_name   VARCHAR(255) NOT NULL,
			customer_phone  VARCHAR(50),
			email           VARCHAR(255),
			booking_date    VARCHAR(20) NOT NULL,
			booking_time    VARCHAR(20) NOT NULL,
			guests          INTEGER DEFAULT 1,
			status          VARCHAR(50) DEFAULT 'PENDING',
			created_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);`,
		// Per-Tenant Email Branding
		`CREATE TABLE IF NOT EXISTS email_templates (
			id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			store_id    UUID REFERENCES stores(id),
			brand_name  VARCHAR(255) NOT NULL,
			brand_address TEXT,
			logo_url    VARCHAR(500),
			primary_color VARCHAR(7) DEFAULT '#ec4899',
			secondary_color VARCHAR(7) DEFAULT '#f97316',
			google_review_url VARCHAR(500),
			UNIQUE(store_id)
		);`,
		// Campaign daily rate limit tracking
		"ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS daily_send_limit INT DEFAULT 500;",
		
		// ─── Phase 5: Comprehensive Multi-Tenant Row Level Security (RLS) ───
		// Standardized on app.current_store_id (set per-request by Go middleware).
		// Drop old policies that used app.tenant_id to avoid conflicts.
		"DROP POLICY IF EXISTS tenant_isolation_customers ON customers;",
		"DROP POLICY IF EXISTS tenant_isolation_orders ON orders;",
		"DROP POLICY IF EXISTS tenant_isolation_campaigns ON campaigns;",
		"DROP POLICY IF EXISTS tenant_isolation_email_templates ON email_templates;",
		"DROP POLICY IF EXISTS tenant_isolation_staff ON staff;",

		// Enable RLS on all 11 tenant-scoped tables
		"ALTER TABLE store_external_mappings ENABLE ROW LEVEL SECURITY;",
		"ALTER TABLE product_prices ENABLE ROW LEVEL SECURITY;",
		"ALTER TABLE orders ENABLE ROW LEVEL SECURITY;",
		"ALTER TABLE staff ENABLE ROW LEVEL SECURITY;",
		"ALTER TABLE shifts ENABLE ROW LEVEL SECURITY;",
		"ALTER TABLE inventory_items ENABLE ROW LEVEL SECURITY;",
		"ALTER TABLE stock_movements ENABLE ROW LEVEL SECURITY;",
		"ALTER TABLE customer_scores ENABLE ROW LEVEL SECURITY;",
		"ALTER TABLE product_recommendations ENABLE ROW LEVEL SECURITY;",
		"ALTER TABLE campaigns ENABLE ROW LEVEL SECURITY;",
		"ALTER TABLE email_templates ENABLE ROW LEVEL SECURITY;",

		// Create RLS policies using app.current_store_id
		"DROP POLICY IF EXISTS tenant_isolation_store_external_mappings ON store_external_mappings;",
		"CREATE POLICY tenant_isolation_store_external_mappings ON store_external_mappings USING (store_id = current_setting('app.current_store_id', true)::uuid);",

		"DROP POLICY IF EXISTS tenant_isolation_product_prices ON product_prices;",
		"CREATE POLICY tenant_isolation_product_prices ON product_prices USING (current_setting('app.current_store_id', true) = '' OR store_id IS NULL OR store_id = current_setting('app.current_store_id', true)::uuid);",

		"DROP POLICY IF EXISTS tenant_isolation_orders ON orders;",
		"CREATE POLICY tenant_isolation_orders ON orders USING (store_id = current_setting('app.current_store_id', true)::uuid);",

		"DROP POLICY IF EXISTS tenant_isolation_staff ON staff;",
		"CREATE POLICY tenant_isolation_staff ON staff USING (store_id = current_setting('app.current_store_id', true)::uuid);",

		"DROP POLICY IF EXISTS tenant_isolation_shifts ON shifts;",
		"CREATE POLICY tenant_isolation_shifts ON shifts USING (store_id = current_setting('app.current_store_id', true)::uuid);",

		"DROP POLICY IF EXISTS tenant_isolation_inventory_items ON inventory_items;",
		"CREATE POLICY tenant_isolation_inventory_items ON inventory_items USING (store_id IS NULL OR store_id = current_setting('app.current_store_id', true)::uuid);",

		"DROP POLICY IF EXISTS tenant_isolation_stock_movements ON stock_movements;",
		"CREATE POLICY tenant_isolation_stock_movements ON stock_movements USING (store_id = current_setting('app.current_store_id', true)::uuid);",

		"DROP POLICY IF EXISTS tenant_isolation_customer_scores ON customer_scores;",
		"CREATE POLICY tenant_isolation_customer_scores ON customer_scores USING (store_id = current_setting('app.current_store_id', true)::uuid);",

		"DROP POLICY IF EXISTS tenant_isolation_product_recommendations ON product_recommendations;",
		"CREATE POLICY tenant_isolation_product_recommendations ON product_recommendations USING (store_id = current_setting('app.current_store_id', true)::uuid);",

		"DROP POLICY IF EXISTS tenant_isolation_campaigns ON campaigns;",
		"CREATE POLICY tenant_isolation_campaigns ON campaigns USING (store_id = current_setting('app.current_store_id', true)::uuid);",

		"DROP POLICY IF EXISTS tenant_isolation_email_templates ON email_templates;",
		"CREATE POLICY tenant_isolation_email_templates ON email_templates USING (store_id = current_setting('app.current_store_id', true)::uuid);",

		// FORCE RLS even for table owner (postgres superuser)
		"ALTER TABLE store_external_mappings FORCE ROW LEVEL SECURITY;",
		"ALTER TABLE product_prices FORCE ROW LEVEL SECURITY;",
		"ALTER TABLE orders FORCE ROW LEVEL SECURITY;",
		"ALTER TABLE staff FORCE ROW LEVEL SECURITY;",
		"ALTER TABLE shifts FORCE ROW LEVEL SECURITY;",
		"ALTER TABLE inventory_items FORCE ROW LEVEL SECURITY;",
		"ALTER TABLE stock_movements FORCE ROW LEVEL SECURITY;",
		"ALTER TABLE customer_scores FORCE ROW LEVEL SECURITY;",
		"ALTER TABLE product_recommendations FORCE ROW LEVEL SECURITY;",
		"ALTER TABLE campaigns FORCE ROW LEVEL SECURITY;",
		"ALTER TABLE email_templates FORCE ROW LEVEL SECURITY;",
	}
	for _, q := range queries {
		if _, err := s.DB.Exec(q); err != nil {
			log.Printf("Migrate failed on query: %s => %v", q, err)
			http.Error(w, `{"error":"migration_failed"}`, http.StatusInternalServerError)
			return
		}
	}

	seedBytes, err := os.ReadFile("migrations/seed.sql")
	if err == nil && len(seedBytes) > 0 {
		// Temporarily disable RLS on all tenant-scoped tables for seeding
		rlsTables := []string{
			"store_external_mappings", "product_prices", "orders", "staff", "shifts",
			"inventory_items", "stock_movements", "customer_scores",
			"product_recommendations", "campaigns", "email_templates",
		}
		for _, t := range rlsTables {
			s.DB.Exec("ALTER TABLE " + t + " DISABLE ROW LEVEL SECURITY")
		}

		if _, err := s.DB.Exec(string(seedBytes)); err != nil {
			log.Printf("Seed failed: %v", err)
			// Re-enable RLS even on failure
			for _, t := range rlsTables {
				s.DB.Exec("ALTER TABLE " + t + " ENABLE ROW LEVEL SECURITY")
				s.DB.Exec("ALTER TABLE " + t + " FORCE ROW LEVEL SECURITY")
			}
			http.Error(w, `{"error":"seed_failed","detail":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		// Re-enable + force RLS after seeding
		for _, t := range rlsTables {
			s.DB.Exec("ALTER TABLE " + t + " ENABLE ROW LEVEL SECURITY")
			s.DB.Exec("ALTER TABLE " + t + " FORCE ROW LEVEL SECURITY")
		}
	}

	w.Write([]byte("Migrations and seeding successful!"))
}

// HandleCfdUpdate receives POS cart state via POST and broadcasts it instantly to connected CFD screens.
func (s *Server) HandleCfdUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Default to Falooda store ID if no header provided
	storeIDStr := r.Header.Get("X-Store-ID")
	if storeIDStr == "" {
		storeIDStr = "f4100da2-1111-1111-1111-000000000001"
	}
	
	storeID, err := uuid.Parse(storeIDStr)
	if err != nil {
		http.Error(w, "Invalid X-Store-ID", http.StatusBadRequest)
		return
	}

	// Read the entire payload 
	// Max ~64KB per update, so BodyLimitMiddleware is good enough
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("CFD update decode error: %v", err)
		http.Error(w, "Invalid CFD payload format", http.StatusBadRequest)
		return
	}

	// Re-encode to send as raw bytes
	bytes, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "Error encoding CFD payload", http.StatusInternalServerError)
		return
	}

	// Broadcast it to the dedicated CFD hub
	s.CfdHub.Broadcast <- CfdMessage{
		StoreID: storeID,
		Payload: bytes,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
