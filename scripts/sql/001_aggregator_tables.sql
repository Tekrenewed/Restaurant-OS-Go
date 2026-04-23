-- Migration: 001_aggregator_tables
-- Description: Creates the tables needed for 0% commission aggregator integration.
--              Includes the idempotency guard table and the menu mapping translation table.

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Table: aggregator_orders
-- Purpose: Prevents double-printing of the same order if delivery networks retry webhooks.
CREATE TABLE IF NOT EXISTS aggregator_orders (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id VARCHAR(50) NOT NULL,
    platform VARCHAR(50) NOT NULL, -- e.g., 'UBEREATS', 'JUSTEAT', 'DELIVEROO'
    external_order_id VARCHAR(100) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    
    -- Composite unique constraint guarantees we only ever store one copy per tenant/platform/order
    CONSTRAINT uq_aggregator_order UNIQUE (tenant_id, platform, external_order_id)
);

-- Table: aggregator_menu_mappings
-- Purpose: Translates third-party menu UUIDs (e.g. Uber Eats' "item-xyz") into our POS internal Product IDs.
CREATE TABLE IF NOT EXISTS aggregator_menu_mappings (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id VARCHAR(50) NOT NULL,
    platform VARCHAR(50) NOT NULL, -- e.g., 'UBEREATS', 'JUSTEAT', 'DELIVEROO'
    external_item_id VARCHAR(100) NOT NULL,
    internal_product_id UUID NOT NULL, -- References internal POS product ID
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    
    -- A single external item ID on a platform maps to exactly one internal product
    CONSTRAINT uq_menu_mapping UNIQUE (tenant_id, platform, external_item_id)
);

-- Optional: Index to speed up the idempotency check
CREATE INDEX IF NOT EXISTS idx_aggregator_orders_lookup 
ON aggregator_orders(tenant_id, platform, external_order_id);

-- Optional: Index to speed up menu mapping translations
CREATE INDEX IF NOT EXISTS idx_menu_mappings_lookup 
ON aggregator_menu_mappings(tenant_id, platform, external_item_id);
