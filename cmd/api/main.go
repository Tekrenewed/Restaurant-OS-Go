package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"restaurant-os/internal/api"
	"restaurant-os/internal/database"
	"restaurant-os/internal/events"
	"restaurant-os/internal/firebase"
	"restaurant-os/internal/integrations"
	"restaurant-os/internal/logger"
	mw "restaurant-os/internal/middleware"
	"runtime/debug"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/joho/godotenv"
)

// ─── Allowed Origins for CORS (no more wildcard) ───
var allowedOrigins = map[string]bool{
	// Falooda & Co
	"https://faloodaandco.web.app":   true,
	"https://faloodaandco.co.uk":     true,
	"https://www.faloodaandco.co.uk": true,
	// Azmos Peri Peri
	"https://azmos-peri-peri.vercel.app": true,
	"https://www.azomsgrill.co.uk":       true,
	"https://azomsgrill.co.uk":           true,
	// Taste of Village Hayes
	"https://hootsnkeks-36451.web.app":    true,
	"https://tasteofvillagehayes.co.uk":   true,
	"https://www.tasteofvillagehayes.co.uk": true,
	// Taste of Village Slough (future — GCP project TBD)
	"https://tasteofvillageslough.web.app": true,
	// Yum Sing
	"https://yumsing.web.app": true,
	// Local development (multiple ports for running brands simultaneously)
	"http://localhost:5173": true, // Vite dev server (Falooda)
	"http://localhost:5174": true, // Vite dev server (Azmos)
	"http://localhost:5175": true, // Vite dev server (TOV)
	"http://localhost:5176": true, // Vite dev server (Yumsing)
	"http://localhost:3000": true, // Next.js dev server (Azmos)
}

// RecoveryMiddleware ensures that a single bad request doesn't crash the entire server
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("PANIC RECOVERED: %v\n%s", err, debug.Stack())
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SecurityHeadersMiddleware adds critical HTTP security headers to every response
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// HSTS — only on production (HTTPS)
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware handles cross-origin requests with a strict origin whitelist
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Store-ID")
		w.Header().Set("Access-Control-Max-Age", "86400") // Cache preflight for 24h
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// BodyLimitMiddleware restricts request body size to prevent memory exhaustion attacks
func BodyLimitMiddleware(maxBytes int64, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next(w, r)
	}
}

// AuthMiddleware verifies Firebase ID tokens for admin-only routes.
// In multi-tenant mode, it resolves which tenant the token belongs to
// and injects a TenantContext into the request context.
// Unauthenticated requests get 401. Invalid/expired tokens get 403.
func AuthMiddleware(fb *firebase.Client, tm *firebase.TenantManager, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"unauthorized","message":"Missing or invalid Authorization header"}`, http.StatusUnauthorized)
			return
		}
		idToken := strings.TrimPrefix(authHeader, "Bearer ")

		ctx := r.Context()

		// Multi-tenant token resolution: try all registered Firebase projects
		if tm != nil {
			tenantClient, token, err := tm.VerifyTokenAndResolveTenant(ctx, idToken)
			if err != nil || token == nil {
				log.Printf("Auth rejected: token not valid for any tenant — %v (from %s)", err, r.RemoteAddr)
				http.Error(w, `{"error":"forbidden","message":"Invalid or expired token"}`, http.StatusForbidden)
				return
			}

			// ENFORCE ROLE-BASED ACCESS CONTROL (RBAC)
			isAdmin, ok := token.Claims["admin"].(bool)
			if !ok || !isAdmin {
				log.Printf("SECURITY ALERT: Standard user %s attempted admin access on tenant %s from %s", token.UID, tenantClient.Config.ProjectID, r.RemoteAddr)
				http.Error(w, `{"error":"forbidden","message":"Insufficient privileges. Admin access required."}`, http.StatusForbidden)
				return
			}

			// Inject tenant context into the request
			tc := &firebase.TenantContext{
				Client:    tenantClient,
				Token:     token,
				StoreID:   tenantClient.Config.StoreID,
				ProjectID: tenantClient.Config.ProjectID,
			}
			r = r.WithContext(firebase.WithTenantContext(ctx, tc))
			log.Printf("Auth OK: user %s on tenant %s (%s)", token.UID, tenantClient.Config.DisplayName, tenantClient.Config.ProjectID)
		} else if fb != nil {
			// Single-tenant fallback (backwards compatible)
			token, err := fb.VerifyAuthToken(ctx, idToken)
			if err != nil || token == nil {
				log.Printf("Auth rejected: invalid token from %s — %v", r.RemoteAddr, err)
				http.Error(w, `{"error":"forbidden","message":"Invalid or expired token"}`, http.StatusForbidden)
				return
			}

			isAdmin, ok := token.Claims["admin"].(bool)
			if !ok || !isAdmin {
				log.Printf("SECURITY ALERT: Standard user %s attempted to access protected Admin API from %s", token.UID, r.RemoteAddr)
				http.Error(w, `{"error":"forbidden","message":"Insufficient privileges. Admin access required."}`, http.StatusForbidden)
				return
			}
		}
		// Token is valid — proceed
		next(w, r)
	}
}

// MigrateKeyMiddleware gates the migrate endpoint behind a secret key
// This prevents anyone from running DB migrations without the deploy key
// Key is sent via X-Migrate-Key header (not query param, to avoid logging exposure)
func MigrateKeyMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(os.Getenv("MIGRATE_KEY"))
		if key == "" {
			// If no key is set, migration is disabled entirely
			http.Error(w, `{"error":"migration_disabled"}`, http.StatusForbidden)
			return
		}
		provided := strings.TrimSpace(r.Header.Get("X-Migrate-Key"))
		if provided != key {
			http.Error(w, `{"error":"invalid_key"}`, http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "restaurant-os"})
}

func main() {
	// Initialize structured JSON logging (Task 9)
	logger.Init()

	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, relying on system environment variables")
	}

	// 1. Initialize Database Connection (PostgreSQL — source of truth)
	db := database.InitDB()

	// 2. Initialize Firebase Admin SDK (Firestore — real-time POS/KDS sync)
	fb := firebase.InitFirebase()

	// 3. Initialize Multi-Tenant Firebase Manager (for Falooda, Roti Naan Wala, and future clients)
	tm := firebase.NewTenantManager()

	// 4. Initialize Real-Time KDS Hub (WebSocket fallback, kept for backwards compat)
	hub := api.NewHub()
	go hub.Run()

	// 5. Initialize Event Bus (Decoupled Real-Time Publisher)
	// Today: Firestore adapter. Future: swap to Redis with zero handler changes.
	var publisher events.RealtimePublisher
	if tm != nil {
		publisher = events.NewFirestorePublisher(func(storeID string) *firestore.Client {
			if storeID != "" {
				if client := tm.GetClientByStoreID(storeID); client != nil {
					return client.Firestore
				}
			}
			if def := tm.GetDefault(); def != nil {
				return def.Firestore
			}
			return nil
		})
		log.Println("Event Bus: FirestorePublisher (multi-tenant)")
	} else {
		publisher = events.NewNoopPublisher()
		log.Println("Event Bus: NoopPublisher (no tenants configured)")
	}

	// 6. Initialize Server Handlers
	server := &api.Server{
		DB:        db,
		Hub:       hub,
		Firebase:  fb,
		Tenants:   tm,
		Publisher: publisher,
	}

	webhookHandler := &integrations.WebhookHandler{
		DB:       db,
		Hub:      hub,
		Firebase: fb,
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Body size limits
	const mb1 = 1 << 20   // 1MB for normal requests
	const kb64 = 64 << 10 // 64KB for small JSON payloads

	mux := http.NewServeMux()

	// ─── Rate Limiters (per-IP sliding window) ───
	pinLimiter := NewRateLimiter(5, time.Minute)    // 5 PIN attempts per minute per IP
	shiftLimiter := NewRateLimiter(10, time.Minute) // 10 clock actions per minute per IP
	orderLimiter := NewRateLimiter(20, time.Minute) // 20 orders per minute per IP
	mailLimiter := NewRateLimiter(5, time.Minute)   // 5 mail triggers per minute per IP
	staffLimiter := NewRateLimiter(20, time.Minute) // 20 staff operations per minute per IP

	// ─── Public Endpoints (no auth required) ───
	mux.HandleFunc("/api/v1/health", healthCheckHandler)
	mux.HandleFunc("/api/v1/catalog", BodyLimitMiddleware(kb64, server.HandleGetFullCatalog))                               // GET — full menu with variants, allergens, modifiers
	mux.HandleFunc("/api/v1/orders", RateLimitMiddleware(orderLimiter, BodyLimitMiddleware(mb1, server.HandleCreateOrder))) // POST — QR/POS ordering

	// ─── Protected Endpoints (Firebase Auth required) ───
	mux.HandleFunc("/api/v1/migrate", MigrateKeyMiddleware(BodyLimitMiddleware(kb64, server.HandleMigrate)))

	mux.HandleFunc("/api/v1/orders/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/status") {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleUpdateOrderStatus))(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/pay") {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleCreatePayment))(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/refund") {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleRefundOrder))(w, r)
			return
		}
		// Generic order update (dual-write: Postgres + Firestore)
		// Used by KDS bump, POS status changes, and WaiterPad updates
		if r.Method == http.MethodPatch || r.Method == http.MethodPost {
			BodyLimitMiddleware(kb64, server.HandleUpdateOrder)(w, r)
			return
		}
		http.NotFound(w, r)
	})

	// ─── Bookings (Dual-Write Postgres + Firestore) ───
	mux.HandleFunc("/api/v1/bookings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			BodyLimitMiddleware(kb64, server.HandleCreateBooking)(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/v1/bookings/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch || r.Method == http.MethodPost {
			BodyLimitMiddleware(kb64, server.HandleUpdateBooking)(w, r)
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/api/v1/stores/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/menu") {
			BodyLimitMiddleware(kb64, server.HandleGetStoreMenu)(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/tables") {
			BodyLimitMiddleware(kb64, server.HandleGetStoreTables)(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/history") {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleGetOrderHistory))(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/orders") {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleGetOrders))(w, r)
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/api/v1/products/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
		// GET /api/v1/products/:id/full — full product detail
		if len(parts) >= 6 && parts[5] == "full" && r.Method == http.MethodGet {
			BodyLimitMiddleware(kb64, server.HandleGetFullProduct)(w, r)
			return
		}
		// POST /api/v1/products/:id/variants — add size variant
		if len(parts) >= 6 && parts[5] == "variants" && r.Method == http.MethodPost {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleAddVariant))(w, r)
			return
		}
		// POST /api/v1/products/:id/groups — add modifier group
		if len(parts) >= 6 && parts[5] == "groups" && r.Method == http.MethodPost {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleAddModifierGroup))(w, r)
			return
		}
		// POST /api/v1/products/:id/allergens — set allergens (replaces all)
		if len(parts) >= 6 && parts[5] == "allergens" && r.Method == http.MethodPost {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleSetAllergens))(w, r)
			return
		}
		// POST /api/v1/products/:id/nutrition — set nutrition info
		if len(parts) >= 6 && parts[5] == "nutrition" && r.Method == http.MethodPost {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleSetNutrition))(w, r)
			return
		}
		// PATCH /api/v1/products/:id/86 — mark item sold out
		if len(parts) >= 5 && strings.HasSuffix(r.URL.Path, "/86") {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.Handle86Product))(w, r)
			return
		}
		http.NotFound(w, r)
	})

	// ─── Modifier Group & Option Management ───
	mux.HandleFunc("/api/v1/groups/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
		// POST /api/v1/groups/:id/options — add option to group
		if len(parts) >= 6 && parts[5] == "options" && r.Method == http.MethodPost {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleAddModifierOption))(w, r)
			return
		}
		http.NotFound(w, r)
	})

	// PATCH /api/v1/options/:id — update a single modifier option
	mux.HandleFunc("/api/v1/options/", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleUpdateModifierOption)))

	// ─── Staff Shift Management (PIN-authenticated, rate-limited) ───
	mux.HandleFunc("/api/v1/shifts/login", RateLimitMiddleware(pinLimiter, BodyLimitMiddleware(kb64, server.HandleShiftLogin)))
	mux.HandleFunc("/api/v1/shifts/clock", RateLimitMiddleware(shiftLimiter, BodyLimitMiddleware(kb64, server.HandleShiftClock)))
	mux.HandleFunc("/api/v1/shifts/active", BodyLimitMiddleware(kb64, server.HandleGetActiveShifts))
	mux.HandleFunc("/api/v1/shifts/history", BodyLimitMiddleware(kb64, server.HandleGetShiftHistory))
	mux.HandleFunc("/api/v1/shifts/export", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleExportTimesheets)))

	// ─── Menu 86 Board ───
	mux.HandleFunc("/api/v1/menu/sold-out", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			BodyLimitMiddleware(kb64, server.HandleGet86Board)(w, r)
		case http.MethodPost:
			BodyLimitMiddleware(kb64, server.HandleToggle86Board)(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// ─── Custom Menu Items (Phase 4 — replaces direct Firestore onSnapshot in AdminPOS) ───
	mux.HandleFunc("/api/v1/menu/custom", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleListCustomMenuItems)))

	// ─── Admin-Only: PIN Migration & Audit Log ───
	mux.HandleFunc("/api/v1/staff/migrate-pins", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleMigratePINs)))
	mux.HandleFunc("/api/v1/audit", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleGetAuditLog)))

	// ─── Staff CRUD (Phase 4 Decoupling — replaces direct Firestore writes from frontend) ───
	mux.HandleFunc("/api/v1/staff", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			AuthMiddleware(fb, tm, RateLimitMiddleware(staffLimiter, BodyLimitMiddleware(kb64, server.HandleListStaff)))(w, r)
		case http.MethodPost:
			AuthMiddleware(fb, tm, RateLimitMiddleware(staffLimiter, BodyLimitMiddleware(kb64, server.HandleCreateStaff)))(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/staff/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/points") {
			AuthMiddleware(fb, tm, RateLimitMiddleware(staffLimiter, BodyLimitMiddleware(kb64, server.HandleIncrementStaffPoints)))(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			AuthMiddleware(fb, tm, RateLimitMiddleware(staffLimiter, BodyLimitMiddleware(kb64, server.HandleDeleteStaff)))(w, r)
			return
		}
		http.NotFound(w, r)
	})

	// ─── Email Dispatch (Phase 4 Decoupling — replaces direct Firestore mail writes) ───
	mux.HandleFunc("/api/v1/mail/send", AuthMiddleware(fb, tm, RateLimitMiddleware(mailLimiter, BodyLimitMiddleware(kb64, server.HandleSendEmail))))

	// ─── Settings CRUD (Phase 4 Decoupling — replaces direct Firestore setDoc from MenuPanel) ───
	mux.HandleFunc("/api/v1/settings/", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleSaveSettings)))

	// ─── Auto Daily Z-Report (triggered by Cloud Scheduler or manual) ───
	mux.HandleFunc("/api/v1/reports/daily", MigrateKeyMiddleware(BodyLimitMiddleware(kb64, server.HandleDailyReport)))

	// ─── Staff Rota / Scheduling (Admin-only CRUD) ───
	mux.HandleFunc("/api/v1/rota", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleGetRota))(w, r)
		case http.MethodPost:
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleSetRota))(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/rota/next", BodyLimitMiddleware(kb64, server.HandleGetStaffNextShift))
	mux.HandleFunc("/api/v1/rota/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleDeleteRota))(w, r)
			return
		}
		http.NotFound(w, r)
	})

	// ─── Loyalty Rewards & CRM Automation ───
	mux.HandleFunc("/api/v1/loyalty/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/redeem") {
			AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleRedeemReward))(w, r)
			return
		}
		BodyLimitMiddleware(kb64, server.HandleGetCustomerRewards)(w, r)
	})
	mux.HandleFunc("/api/v1/crm/dispatch-reward", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleDispatchReward)))

	// ─── CRM Customer API (for POS lookup + silent capture) ───
	mux.HandleFunc("/api/v1/customers/lookup", BodyLimitMiddleware(kb64, api.HandleGetCustomer(db)))
	mux.HandleFunc("/api/v1/customers", BodyLimitMiddleware(kb64, server.HandleCreateCustomerWithSync))

	// ─── Health Probes ───
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// ─── SaaS Internal Workers (Cloud Scheduler endpoints, protected by MigrateKey) ───
	mux.HandleFunc("/api/v1/internal/migrate-customers", MigrateKeyMiddleware(BodyLimitMiddleware(kb64, server.HandleMigrateFirestoreCustomers)))
	mux.HandleFunc("/api/v1/internal/score-customers", MigrateKeyMiddleware(BodyLimitMiddleware(kb64, server.HandleRecalculateScores)))
	mux.HandleFunc("/api/v1/internal/build-recommendations", MigrateKeyMiddleware(BodyLimitMiddleware(kb64, server.HandleBuildRecommendations)))
	// Week 3: mux.HandleFunc("/api/v1/internal/execute-campaigns", MigrateKeyMiddleware(BodyLimitMiddleware(kb64, server.HandleExecuteScheduledCampaigns)))

	// ─── Segments & Recommendations (Admin Dashboard APIs) ───
	mux.HandleFunc("/api/v1/segments", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleGetSegments)))
	mux.HandleFunc("/api/v1/customers/score", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleGetCustomerScore)))
	mux.HandleFunc("/api/v1/recommendations", BodyLimitMiddleware(kb64, server.HandleGetRecommendations))

	// KDS Sockets (Connected to React Screen)
	mux.HandleFunc("/ws", hub.ServeWs)

	// ─── AI Media Studio ───
	mux.HandleFunc("/api/v1/ai/generate", AuthMiddleware(fb, tm, BodyLimitMiddleware(mb1, server.HandleGenerateAIMedia)))
	mux.HandleFunc("/api/v1/ai/trends", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandleGetViralTrends)))
	mux.HandleFunc("/api/v1/ai/publish/instagram", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandlePublishToInstagram)))
	mux.HandleFunc("/api/v1/ai/publish/tiktok", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, server.HandlePublishToTikTok)))
	mux.HandleFunc("/api/v1/internal/cron/scrape-trends", MigrateKeyMiddleware(BodyLimitMiddleware(kb64, server.HandleScrapeViralTrendsCron)))

	// Direct Delivery Integrations (No Middleman)
	mux.HandleFunc("/webhooks/deliveroo", BodyLimitMiddleware(mb1, webhookHandler.HandleDeliverooWebhook))
	mux.HandleFunc("/webhooks/ubereats", BodyLimitMiddleware(mb1, webhookHandler.HandleUberEatsWebhook))
	mux.HandleFunc("/webhooks/justeat", BodyLimitMiddleware(mb1, webhookHandler.HandleJustEatWebhook))
	mux.HandleFunc("/webhooks/dojo", BodyLimitMiddleware(mb1, webhookHandler.HandleDojoWebhook))

	// Manual Payment Verification (for POS UI)
	mux.HandleFunc("/api/v1/payments/verify", AuthMiddleware(fb, tm, BodyLimitMiddleware(kb64, webhookHandler.HandleVerifyPayment)))

	// ─── Apply middleware chain: Security → CORS → Recovery → Timeout → Logger → Router ───
	handlerChain := SecurityHeadersMiddleware(
		CORSMiddleware(
			RecoveryMiddleware(
				mw.Timeout(15 * time.Second)(
					mw.RequestLogger(mux),
				),
			),
		),
	)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handlerChain,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("Restaurant OS backend starting on port %s (CORS: whitelist mode, Auth: Firebase)", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
