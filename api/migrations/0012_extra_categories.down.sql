-- Drop expanded categories. If any expense or recurring template references
-- one of these slugs the FK will block this rollback, which is intentional:
-- categories carry historical data and shouldn't disappear under a row.
DELETE FROM categories WHERE slug IN (
    'games', 'movies', 'music', 'sports',
    'dining_out', 'liquor',
    'electronics', 'furniture', 'household_supplies', 'maintenance',
    'mortgage', 'rent', 'pets', 'services',
    'childcare', 'clothing', 'education', 'gifts', 'insurance', 'medical', 'taxes', 'loan',
    'bicycle', 'train', 'bus', 'car', 'fuel', 'hotel', 'parking', 'plane', 'taxi',
    'cleaning', 'electricity', 'heating_gas', 'trash', 'tv', 'internet', 'phone', 'water'
);
