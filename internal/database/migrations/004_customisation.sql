-- ═══════════════════════════════════════════════════════════════════════════
-- Migration 004: Full Product Customisation System
-- Falooda & Co — April 2026
-- Adds: product_variants, modifier_groups, modifier_options,
--       product_allergens, product_nutrition
--       Also adds: image_url, is_active, sort_order to products
-- ═══════════════════════════════════════════════════════════════════════════

-- A) Extend products table
ALTER TABLE products
  ADD COLUMN IF NOT EXISTS image_url VARCHAR(500),
  ADD COLUMN IF NOT EXISTS is_active BOOLEAN DEFAULT true,
  ADD COLUMN IF NOT EXISTS sort_order INT DEFAULT 0;

-- B) Product Variants (sizes: Small / Regular / Large / Single Scoop etc.)
CREATE TABLE IF NOT EXISTS product_variants (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id    UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    name          VARCHAR(100) NOT NULL,        -- 'Small', 'Regular', 'Large'
    price_override DECIMAL(10,2) NOT NULL,      -- Absolute price for this variant
    is_default    BOOLEAN DEFAULT false,         -- Pre-selected for customer
    sort_order    INT DEFAULT 0,
    is_active     BOOLEAN DEFAULT true,
    UNIQUE(product_id, name)
);

-- C) Modifier Groups
-- A product can have multiple groups (e.g. "Sauces", "Extras", "Meal Upgrade")
CREATE TABLE IF NOT EXISTS modifier_groups (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id     UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    name           VARCHAR(100) NOT NULL,        -- 'Choose your sauce'
    description    VARCHAR(255),                 -- Helper text shown to customer
    is_required    BOOLEAN DEFAULT false,         -- Customer MUST pick something
    min_selections INT DEFAULT 0,
    max_selections INT DEFAULT 1,                -- 1 = radio buttons, >1 = checkboxes
    sort_order     INT DEFAULT 0,
    is_active      BOOLEAN DEFAULT true
);

-- D) Modifier Options (individual choices within a group)
CREATE TABLE IF NOT EXISTS modifier_options (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id     UUID NOT NULL REFERENCES modifier_groups(id) ON DELETE CASCADE,
    name         VARCHAR(100) NOT NULL,           -- 'No Mayo', 'Extra Cheese'
    price_delta  DECIMAL(10,2) DEFAULT 0.00,      -- 0 = free, +0.50 = upcharge
    is_default   BOOLEAN DEFAULT false,            -- Pre-ticked
    is_available BOOLEAN DEFAULT true,             -- 86 individual options
    sort_order   INT DEFAULT 0
);

-- E) Allergens (UK Natasha's Law — 14 major allergens)
-- Severity: 'contains' or 'may_contain'
CREATE TABLE IF NOT EXISTS product_allergens (
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    allergen   VARCHAR(50) NOT NULL,
    -- Valid values: celery, cereals_gluten, crustaceans, eggs, fish,
    --               lupin, milk, molluscs, mustard, nuts, peanuts,
    --               sesame, soya, sulphur_dioxide
    severity   VARCHAR(20) NOT NULL DEFAULT 'contains',
    PRIMARY KEY (product_id, allergen)
);

-- F) Nutritional Information (per serving)
CREATE TABLE IF NOT EXISTS product_nutrition (
    product_id   UUID PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
    serving_size_g  INT,
    calories        INT,
    fat_g           DECIMAL(6,2),
    saturates_g     DECIMAL(6,2),
    carbs_g         DECIMAL(6,2),
    sugars_g        DECIMAL(6,2),
    fibre_g         DECIMAL(6,2),
    protein_g       DECIMAL(6,2),
    salt_g          DECIMAL(6,2)
);

-- ── Indexes for performance ──────────────────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_product_variants_product_id ON product_variants(product_id);
CREATE INDEX IF NOT EXISTS idx_modifier_groups_product_id  ON modifier_groups(product_id);
CREATE INDEX IF NOT EXISTS idx_modifier_options_group_id   ON modifier_options(group_id);
CREATE INDEX IF NOT EXISTS idx_product_allergens_product   ON product_allergens(product_id);
