-- Phase 2 Database Migrations

-- 1. Create tables table
CREATE TABLE IF NOT EXISTS tables (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id UUID REFERENCES stores(id),
    table_number INT NOT NULL,
    is_active BOOLEAN DEFAULT true,
    UNIQUE(store_id, table_number)
);

-- 2. Alter orders table to support QR/Collection info
-- Use DO block to handle conditional column addition to avoid errors
DO $$ 
BEGIN 
    BEGIN
        ALTER TABLE orders ADD COLUMN table_number INT;
    EXCEPTION
        WHEN duplicate_column THEN RAISE NOTICE 'column table_number already exists in orders.';
    END;

    BEGIN
        ALTER TABLE orders ADD COLUMN customer_name VARCHAR(255);
    EXCEPTION
        WHEN duplicate_column THEN RAISE NOTICE 'column customer_name already exists in orders.';
    END;

    BEGIN
        ALTER TABLE orders ADD COLUMN customer_phone VARCHAR(20);
    EXCEPTION
        WHEN duplicate_column THEN RAISE NOTICE 'column customer_phone already exists in orders.';
    END;
END $$;

-- 3. Alter products for 86'd feature
DO $$ 
BEGIN 
    BEGIN
        ALTER TABLE products ADD COLUMN is_86d BOOLEAN DEFAULT false;
    EXCEPTION
        WHEN duplicate_column THEN RAISE NOTICE 'column is_86d already exists in products.';
    END;
END $$;
