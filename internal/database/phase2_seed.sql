-- Seed Falooda & Co brand and tables

-- 1. Insert Falooda & Co brand
INSERT INTO brands (id, name, website_url) VALUES 
('f4100da2-0000-0000-0000-000000000001', 'Falooda & Co', 'https://faloodaandco.co.uk')
ON CONFLICT (id) DO NOTHING;

-- 2. Insert Falooda & Co Store (Farnham Road)
INSERT INTO stores (id, brand_id, name, address) VALUES 
('f4100da2-1111-1111-1111-000000000001', 'f4100da2-0000-0000-0000-000000000001', 'Falooda & Co - Main', '268 Farnham Road, Slough, Berkshire, SL1 4XL')
ON CONFLICT (id) DO NOTHING;

-- 3. Seed 10 Tables for Falooda & Co (8 inside, 2 outside)
INSERT INTO tables (store_id, table_number) VALUES 
('f4100da2-1111-1111-1111-000000000001', 1),
('f4100da2-1111-1111-1111-000000000001', 2),
('f4100da2-1111-1111-1111-000000000001', 3),
('f4100da2-1111-1111-1111-000000000001', 4),
('f4100da2-1111-1111-1111-000000000001', 5),
('f4100da2-1111-1111-1111-000000000001', 6),
('f4100da2-1111-1111-1111-000000000001', 7),
('f4100da2-1111-1111-1111-000000000001', 8),
('f4100da2-1111-1111-1111-000000000001', 9),
('f4100da2-1111-1111-1111-000000000001', 10)
ON CONFLICT (store_id, table_number) DO NOTHING;

-- Note: We assume the menu will be seeded from the website later through an admin portal
-- or we can use the existing `constants.ts` to build out the SQL menu later.
