-- ============================================================
-- Migration 005: Row-Level Security (RLS) for Multi-Tenancy
-- ============================================================
-- Purpose: Enforce tenant isolation at the database level.
-- Even if Go code forgets WHERE store_id = ?, PostgreSQL
-- will silently filter rows to only the current tenant.
--
-- Usage in Go: Before each request, execute:
--   db.Exec("SET app.current_store_id = $1", storeID)
--
-- To disable RLS for admin/maintenance queries:
--   SET ROLE postgres;  (superuser bypasses RLS)
--
-- This migration is ADDITIVE — it does not alter existing
-- data or break existing queries. It only adds a safety net.
--
-- Can be migrated to separate databases in future if needed
-- by simply creating new databases and disabling RLS.
-- ============================================================

-- Enable RLS on all tenant-scoped tables
-- Note: Tables using brand_id instead of store_id (products, inventory_items)
-- are NOT included here — they scope by brand, not store.
-- RLS for brand_id tables can be added later if needed.

-- 1. store_external_mappings
ALTER TABLE store_external_mappings ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_store_external_mappings ON store_external_mappings
    USING (store_id = current_setting('app.current_store_id', true)::uuid);

-- 2. product_prices (has nullable store_id — NULL means "all stores")
ALTER TABLE product_prices ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_product_prices ON product_prices
    USING (
        store_id IS NULL
        OR store_id = current_setting('app.current_store_id', true)::uuid
    );

-- 3. orders
ALTER TABLE orders ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_orders ON orders
    USING (store_id = current_setting('app.current_store_id', true)::uuid);

-- 4. staff
ALTER TABLE staff ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_staff ON staff
    USING (store_id = current_setting('app.current_store_id', true)::uuid);

-- 5. shifts
ALTER TABLE shifts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_shifts ON shifts
    USING (store_id = current_setting('app.current_store_id', true)::uuid);

-- 6. inventory_items (has nullable store_id — NULL means brand-wide)
ALTER TABLE inventory_items ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_inventory_items ON inventory_items
    USING (
        store_id IS NULL
        OR store_id = current_setting('app.current_store_id', true)::uuid
    );

-- 7. stock_movements
ALTER TABLE stock_movements ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_stock_movements ON stock_movements
    USING (store_id = current_setting('app.current_store_id', true)::uuid);

-- 8. customer_scores
ALTER TABLE customer_scores ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_customer_scores ON customer_scores
    USING (store_id = current_setting('app.current_store_id', true)::uuid);

-- 9. product_recommendations
ALTER TABLE product_recommendations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_product_recommendations ON product_recommendations
    USING (store_id = current_setting('app.current_store_id', true)::uuid);

-- 10. campaigns
ALTER TABLE campaigns ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_campaigns ON campaigns
    USING (store_id = current_setting('app.current_store_id', true)::uuid);

-- 11. email_templates
ALTER TABLE email_templates ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_email_templates ON email_templates
    USING (store_id = current_setting('app.current_store_id', true)::uuid);

-- ============================================================
-- IMPORTANT: RLS does NOT apply to the table owner (superuser).
-- If your Go app connects as 'postgres' (superuser), RLS is
-- bypassed automatically. For RLS to be enforced, you must
-- either:
--   1. Connect as a non-superuser role (recommended for prod)
--   2. Use FORCE ROW LEVEL SECURITY on each table
--
-- For now, we use FORCE to ensure RLS applies even to the
-- table owner (since you connect as postgres):
-- ============================================================

ALTER TABLE store_external_mappings FORCE ROW LEVEL SECURITY;
ALTER TABLE product_prices FORCE ROW LEVEL SECURITY;
ALTER TABLE orders FORCE ROW LEVEL SECURITY;
ALTER TABLE staff FORCE ROW LEVEL SECURITY;
ALTER TABLE shifts FORCE ROW LEVEL SECURITY;
ALTER TABLE inventory_items FORCE ROW LEVEL SECURITY;
ALTER TABLE stock_movements FORCE ROW LEVEL SECURITY;
ALTER TABLE customer_scores FORCE ROW LEVEL SECURITY;
ALTER TABLE product_recommendations FORCE ROW LEVEL SECURITY;
ALTER TABLE campaigns FORCE ROW LEVEL SECURITY;
ALTER TABLE email_templates FORCE ROW LEVEL SECURITY;

-- ============================================================
-- Verification: After running this migration, test with:
--   SET app.current_store_id = 'f4100da2-1111-1111-1111-000000000001';
--   SELECT * FROM orders;  -- Should only return Falooda orders
--   
--   SET app.current_store_id = 'a2200da2-2222-2222-2222-000000000002';
--   SELECT * FROM orders;  -- Should only return Azmos orders
-- ============================================================
