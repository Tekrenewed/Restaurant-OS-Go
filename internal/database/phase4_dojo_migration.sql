-- Phase 4: Dojo Cloud Payment Integration
-- Adds terminal mapping to stores for automated card payments

-- 1. Add Dojo Terminal ID to stores table
DO $$
BEGIN
    BEGIN
        ALTER TABLE stores ADD COLUMN dojo_terminal_id VARCHAR(255);
    EXCEPTION
        WHEN duplicate_column THEN RAISE NOTICE 'column dojo_terminal_id already exists in stores.';
    END;
END $$;

-- 2. Add dojo_intent_id to orders for tracking payment intents
DO $$
BEGIN
    BEGIN
        ALTER TABLE orders ADD COLUMN dojo_intent_id VARCHAR(255);
    EXCEPTION
        WHEN duplicate_column THEN RAISE NOTICE 'column dojo_intent_id already exists in orders.';
    END;
END $$;
