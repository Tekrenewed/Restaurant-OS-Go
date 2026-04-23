-- Phase 3 Database Migrations
-- Adds phone column to stores and creates external platform mapping table

-- 1. Add phone column to stores table (needed for Uber Direct pickup details)
DO $$
BEGIN
    BEGIN
        ALTER TABLE stores ADD COLUMN phone VARCHAR(20);
    EXCEPTION
        WHEN duplicate_column THEN RAISE NOTICE 'column phone already exists in stores.';
    END;
END $$;

-- 2. Create store_external_mappings table
-- Maps delivery platform location IDs to our internal store UUIDs
CREATE TABLE IF NOT EXISTS store_external_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id UUID NOT NULL REFERENCES stores(id),
    platform VARCHAR(50) NOT NULL,  -- 'Deliveroo', 'UberEats', 'JustEat'
    external_id VARCHAR(255) NOT NULL,  -- The platform's location/restaurant ID
    UNIQUE(platform, external_id)
);

-- 3. Update Falooda & Co store with real address and phone
UPDATE stores
SET address = '268 Farnham Road, Slough, Berkshire, SL1 4XL',
    phone = '+441753326400'
WHERE id = 'f4100da2-1111-1111-1111-000000000001';
