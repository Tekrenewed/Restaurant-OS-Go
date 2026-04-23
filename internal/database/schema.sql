-- 1. Identity & Structure
CREATE TABLE IF NOT EXISTS brands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL, -- e.g., 'Taste of Village', 'Azmoz', 'Falood & Co'
    website_url VARCHAR(255)
);

CREATE TABLE IF NOT EXISTS stores (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id UUID REFERENCES brands(id),
    name VARCHAR(255) NOT NULL, -- e.g., 'Hayes Branch', 'Slough Branch'
    address TEXT,
    phone VARCHAR(20),
    printer_ip_address INET -- For direct Go -> Toast TP200 printing
);

-- Maps delivery platform location IDs to internal store UUIDs
CREATE TABLE IF NOT EXISTS store_external_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id UUID NOT NULL REFERENCES stores(id),
    platform VARCHAR(50) NOT NULL,  -- 'Deliveroo', 'UberEats', 'JustEat'
    external_id VARCHAR(255) NOT NULL,
    UNIQUE(platform, external_id)
);

-- 2. Menu Logic
CREATE TABLE IF NOT EXISTS products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id UUID REFERENCES brands(id),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    category VARCHAR(50) -- 'Curry', 'Dessert', 'Grill', 'Chinese'
);

-- 3. Multi-Tier Pricing
CREATE TABLE IF NOT EXISTS price_levels (
    id SERIAL PRIMARY KEY,
    name VARCHAR(50) NOT NULL -- 'Dine-In', 'Takeaway', 'UberEats', 'Web'
);

CREATE TABLE IF NOT EXISTS product_prices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID REFERENCES products(id),
    store_id UUID REFERENCES stores(id), -- Optional
    price_level_id INT REFERENCES price_levels(id),
    price_amount DECIMAL(10, 2) NOT NULL,
    is_active BOOLEAN DEFAULT true,
    UNIQUE(product_id, store_id, price_level_id)
);

-- 4. Modifiers & Variations (e.g., +£1.99 for Large)
CREATE TABLE IF NOT EXISTS modifiers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID REFERENCES products(id),
    name VARCHAR(255) NOT NULL, -- e.g., 'Large Portion'
    upcharge_amount DECIMAL(10, 2) DEFAULT 0.00
);

-- 5. Order Management
CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id UUID REFERENCES stores(id),
    order_source VARCHAR(50), -- 'POS', 'Web', 'UberEats', 'Deliveroo', 'JustEat'
    net_total DECIMAL(12,2) DEFAULT 0.00,
    vat_total DECIMAL(12,2) DEFAULT 0.00,
    service_charge DECIMAL(12,2) DEFAULT 0.00,
    gross_total DECIMAL(12,2) DEFAULT 0.00,
    status VARCHAR(20) DEFAULT 'pending', -- 'pending', 'kitchen', 'completed', 'paid'
    needs_printing BOOLEAN DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 6. Order Items
CREATE TABLE IF NOT EXISTS order_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID REFERENCES orders(id),
    product_id UUID REFERENCES products(id),
    name VARCHAR(255) NOT NULL,
    price_paid DECIMAL(12,2) NOT NULL,
    is_takeaway BOOLEAN DEFAULT false,
    vat_rate DECIMAL(4,2) NOT NULL
);

-- 7. Customers (CRM)
CREATE TABLE IF NOT EXISTS customers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    short_id VARCHAR(8) UNIQUE DEFAULT upper(substring(md5(random()::text), 1, 6)),
    phone VARCHAR(20) UNIQUE NOT NULL,
    email VARCHAR(255),
    name VARCHAR(255),
    loyalty_points INT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 8. Staff & Rota
CREATE TABLE IF NOT EXISTS staff (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id UUID REFERENCES stores(id),
    name VARCHAR(255) NOT NULL,
    role VARCHAR(50),
    pin VARCHAR(10),
    hourly_rate DECIMAL(10,2),
    is_active BOOLEAN DEFAULT true
);

CREATE TABLE IF NOT EXISTS shifts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    staff_id UUID REFERENCES staff(id),
    store_id UUID REFERENCES stores(id),
    start_time TIMESTAMP WITH TIME ZONE,
    end_time TIMESTAMP WITH TIME ZONE,
    break_minutes INT DEFAULT 0,
    status VARCHAR(20) DEFAULT 'scheduled'
);

-- 9. Inventory Management (Multi-Tenant & Decoupled)
CREATE TABLE IF NOT EXISTS inventory_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    brand_id UUID REFERENCES brands(id), -- Multi-tenant: Belongs to a master brand
    store_id UUID REFERENCES stores(id), -- Multi-tenant: Specific to a physical store (nullable if brand-wide item)
    name VARCHAR(255) NOT NULL,
    unit VARCHAR(20), -- 'kg', 'litres', 'units', 'grams'
    current_stock DECIMAL(12,2) DEFAULT 0.00,
    min_stock_level DECIMAL(12,2) DEFAULT 0.00,
    cost_per_unit DECIMAL(10,2) DEFAULT 0.00,
    last_updated TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS product_ingredients (
    product_id UUID REFERENCES products(id),
    item_id UUID REFERENCES inventory_items(id),
    quantity_per_unit DECIMAL(10,4), -- e.g., 0.200 kg flour per product
    PRIMARY KEY(product_id, item_id)
);

CREATE TABLE IF NOT EXISTS stock_movements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id UUID REFERENCES inventory_items(id),
    store_id UUID REFERENCES stores(id), -- Tracks exactly which store used/wasted it
    quantity DECIMAL(12,2) NOT NULL,
    movement_type VARCHAR(50), -- 'purchase', 'usage', 'waste', 'transfer', 'adjustment'
    notes TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 10. RFM Customer Scoring (populated nightly by Cloud Scheduler)
CREATE TABLE IF NOT EXISTS customer_scores (
    customer_id UUID PRIMARY KEY REFERENCES customers(id),
    store_id    UUID REFERENCES stores(id),
    recency_score   INT DEFAULT 0,
    frequency_score INT DEFAULT 0,
    monetary_score  INT DEFAULT 0,
    total_score     INT DEFAULT 0,
    segment         VARCHAR(20) DEFAULT 'NEW', -- 'VIP', 'REGULAR', 'CHURN_RISK', 'NEW'
    updated_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 11. Co-Purchase Recommendations (populated nightly by Cloud Scheduler)
CREATE TABLE IF NOT EXISTS product_recommendations (
    product_id             UUID REFERENCES products(id),
    recommended_product_id UUID REFERENCES products(id),
    store_id               UUID REFERENCES stores(id),
    co_purchase_count      INT DEFAULT 0,
    PRIMARY KEY (product_id, recommended_product_id, store_id)
);

-- 12. Campaign History & Scheduling (Central Campaign Engine)
CREATE TABLE IF NOT EXISTS campaigns (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id    UUID REFERENCES stores(id),
    name        VARCHAR(255) NOT NULL,
    segment     VARCHAR(20) NOT NULL,         -- 'VIP', 'REGULAR', 'CHURN_RISK', 'ALL'
    offer_type  VARCHAR(50) NOT NULL,         -- 'percent_discount', 'fixed_discount', 'free_item', 'custom'
    offer_value DECIMAL(10,2),
    offer_code  VARCHAR(50),
    channel     VARCHAR(20) DEFAULT 'email',  -- 'email', 'sms', 'whatsapp'
    message_html TEXT,
    status      VARCHAR(20) DEFAULT 'draft',  -- 'draft', 'scheduled', 'sent', 'failed'
    scheduled_at TIMESTAMP WITH TIME ZONE,
    sent_at     TIMESTAMP WITH TIME ZONE,
    recipients_count INT DEFAULT 0,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 13. Email Template Branding (per-tenant, for multi-restaurant SaaS)
CREATE TABLE IF NOT EXISTS email_templates (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id    UUID REFERENCES stores(id),
    brand_name  VARCHAR(255) NOT NULL,
    brand_address TEXT,
    logo_url    VARCHAR(500),
    primary_color VARCHAR(7) DEFAULT '#ec4899',
    secondary_color VARCHAR(7) DEFAULT '#f97316',
    google_review_url VARCHAR(500),
    UNIQUE(store_id)
);
