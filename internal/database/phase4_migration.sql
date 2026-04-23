-- Phase 4 Database Migration
-- Adds gamification points to staff table and ensures store_id is nullable for tenant resolution

-- 1. Add points column to staff table (for gamification)
DO $$
BEGIN
    BEGIN
        ALTER TABLE staff ADD COLUMN points INT DEFAULT 0;
    EXCEPTION
        WHEN duplicate_column THEN RAISE NOTICE 'column points already exists in staff.';
    END;
END $$;

-- 2. Add pin_hash column to staff table (for bcrypt migration)
DO $$
BEGIN
    BEGIN
        ALTER TABLE staff ADD COLUMN pin_hash VARCHAR(255);
    EXCEPTION
        WHEN duplicate_column THEN RAISE NOTICE 'column pin_hash already exists in staff.';
    END;
END $$;

-- 3. Add source column to customers table (tracks how they signed up)
DO $$
BEGIN
    BEGIN
        ALTER TABLE customers ADD COLUMN source VARCHAR(50) DEFAULT 'web';
    EXCEPTION
        WHEN duplicate_column THEN RAISE NOTICE 'column source already exists in customers.';
    END;
END $$;

-- 4. Add store_id to customers (multi-tenant CRM)
DO $$
BEGIN
    BEGIN
        ALTER TABLE customers ADD COLUMN store_id UUID REFERENCES stores(id);
    EXCEPTION
        WHEN duplicate_column THEN RAISE NOTICE 'column store_id already exists in customers.';
    END;
END $$;
