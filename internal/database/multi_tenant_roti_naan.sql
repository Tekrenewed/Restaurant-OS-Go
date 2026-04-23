-- Multitenant Setup for Roti Naan Wala
INSERT INTO brands (id, name, website_url) VALUES 
('cb5a1d1e-1bf3-4171-8453-a950b87ff9c1', 'Roti Naan Wala', 'https://rotinaanwala.co.uk')
ON CONFLICT DO NOTHING;

INSERT INTO stores (id, brand_id, name, address) VALUES 
('cb5a1d1e-1bf3-4171-8453-a950b87ff9c2', 'cb5a1d1e-1bf3-4171-8453-a950b87ff9c1', 'Roti Naan Wala Slough', 'Slough, UK')
ON CONFLICT DO NOTHING;
