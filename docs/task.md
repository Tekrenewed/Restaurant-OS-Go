# Restaurant OS - Project Breakdown

## Phase 1: Foundation & Backend Setup
- [x] Initialize `restaurant-os` Go module
- [x] Set up HTTP server router and Panic Recovery Middleware
- [x] Integrate PostgreSQL connection (`github.com/lib/pq`, `github.com/jmoiron/sqlx`)
- [x] Write SQL migration scripts for multi-tenant schema
  - [x] `brands`, `stores`, `products`
  - [x] `price_levels`, `product_prices`, `modifiers`
  - [x] `orders`

## Phase 2: Core Financial & Business Logic
- [x] Implement UK Tax Calculator (20% Standard, 0% Zero-Rated)
- [x] Implement toggleable Discretionary Service Charge (12.5%)
- [x] Implement basic Create Order endpoint (`POST /api/v1/orders`) includes calculating totals

## Phase 3: Real-Time Kitchen Display System (KDS)
- [x] Build Go WebSocket Hub for order broadcasting
- [x] Create KDS React frontend (lightweight setup)
- [x] Implement station filtering and targeted KDS dispatching based on `store_id`

## Phase 4: Printer & Hardware Integrations
- [x] Add ESC/POS package for Toast TP200 communication over LAN
- [x] Build Dojo API Payment Intent logic in Go

## Phase 5: Direct Delivery Platform Integration
- [x] Configure unified Webhook Receiver for UberEats, Deliveroo, and JustEat
- [ ] Implement Idempotency Layer to prevent duplicate delivery orders
- [x] Setup defensive payload parsing strategies (Panic Recovery for garbage data)
- [x] Integrate Uber Direct API for white-label delivery driver dispatch (Yum Sing Website)

## Phase 6: Admin Control Center (Unified Menu Manager)
- [x] Build `GET /api/v1/catalog`, `GET /api/v1/stores/{id}/menu` APIs
- [x] Build React Admin UI for price toggles and platform syncing

## Phase 7: Point of Sale (POS) Interface
- [x] Build React POS Screen (`frontend/src/pos/POS.tsx`)
- [x] Connect POS to `GET /api/v1/catalog` to display menu
- [x] Implement POS Cart state mapping and Total Calculations (20% VAT + optional 12.5% Service)
- [x] Connect POS Cart directly to `POST /api/v1/orders` to instantaneously dispatch orders to the KDS WebSockets.
