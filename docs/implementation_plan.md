# Restaurant OS Blueprint

## Goal Description
Build an end-to-end, multi-tenant POS and Restaurant Operating System in Go to manage 4 distinct branches: Taste of Village Hayes, Taste of Village Slough, Azmoz (incorporating Yum Sing), and Falood & Co. The system relies on a central GCP-hosted Go backend with real-time WebSocket communication, multi-tenant PostgreSQL databases, direct REST integration with Dojo payment terminals, ESC/POS hardware (Toast TP200), and custom Webhooks for delivery platforms (excluding third-party aggregators).

## User Review Required
> [!IMPORTANT]
> - **Codebase Location**: Should I initialize the Go module in a new folder like `C:\Users\TKR\Desktop\Taste Of Village\restaurant-os`?
> - **Frontend**: Should the React Admin UI and KDS apps be part of a monorepo (in the same folder under a `/frontend` directory) or outside the Go project?
> - **Database Testing**: During development, is it okay to use a local Docker-based PostgreSQL database (aligning with your Local-First standard) or do you want to connect directly to GCP Cloud SQL immediately?

## Proposed Changes

### Server Foundation
- **[NEW]** `go.mod` and `cmd/api/main.go`. Entry points containing the Panic Recovery middleware.
- **[NEW]** `internal/database/schema.sql` defining multi-brand relationships (`brands`, `stores`, `products`, `price_levels`, `product_prices`, `orders`).
- **[NEW]** `internal/database/db.go` connecting to Postgres using `sqlx`.

### Financial logic
- **[NEW]** `pkg/calculator/tax.go` implementing the UK VAT logic (20% standard, 0% zero-rated for cold takeaway) and toggleable 12.5% Service Charge logic.

### KDS & WebSockets
- **[NEW]** `internal/api/hub.go` containing the WebSocket hub state to broadcast orders directly to connected KDS React clients per `store_id`.

### Payment & Hardware Integration
- **[NEW]** `internal/integrations/dojo.go` for Dojo Payment Intent generation.
- **[NEW]** `pkg/escpos/printer.go` for network socket-based printing to Toast TP200 devices.

### Delivery Integration
- **[NEW]** `internal/integrations/webhooks.go` for capturing and standardizing order payloads from Uber Eats, Deliveroo, and Just Eat, protected by an Idempotency Layer lock mechanism.

### React POS Integration
- **[NEW]** `frontend/src/pos/POS.tsx`. A modern, interactive register screen for taking orders dynamically. The cart logic will compute the 20% VAT in browser (matching the backend engine's logic) before submitting the final structural JSON array to `POST /api/v1/orders`.
- **[MODIFY]** `frontend/src/App.tsx` and Sidebar routing to include the POS register. 

## Verification Plan
### Automated Tests
- Write unit tests in Go (`go test ./...`) for `pkg/calculator/tax_test.go` to guarantee complex UK tax calculations are infallible.
- Add Go tests for WebSocket Hub logic to ensure proper isolated message routing per store.

### Manual Verification
- Spin up a local Postgres instance to verify multi-tenant SQL queries perform as expected without data leaks across brands.
- Test WebSocket KDS hubs with a simple React dummy client.
- **POS Test**: Open a browser tab to the POS, add items to Cart, click "Send to Kitchen", and verify that a second browser tab looking at the KDS screen immediately slides the ticket in.
