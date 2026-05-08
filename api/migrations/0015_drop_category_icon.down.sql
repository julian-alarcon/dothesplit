-- Roll back: re-add the icon column and restore each row's value to match
-- migration 0014. NULLABLE on rollback because we don't want to backfill
-- before populating.

ALTER TABLE categories ADD COLUMN icon TEXT;

-- Entertainment
UPDATE categories SET icon = 'book'           WHERE slug = 'books';
UPDATE categories SET icon = 'microphone'     WHERE slug = 'concerts';
UPDATE categories SET icon = 'gamepad'        WHERE slug = 'games';
UPDATE categories SET icon = 'palette'        WHERE slug = 'hobbies';
UPDATE categories SET icon = 'film'           WHERE slug = 'movies';
UPDATE categories SET icon = 'music'          WHERE slug = 'music';
UPDATE categories SET icon = 'futbol'         WHERE slug = 'sports';
UPDATE categories SET icon = 'masks-theater'  WHERE slug = 'theater';

-- Food & drink
UPDATE categories SET icon = 'cookie-bite'    WHERE slug = 'snacks';
UPDATE categories SET icon = 'utensils'       WHERE slug = 'dining_out';
UPDATE categories SET icon = 'wine-glass'     WHERE slug = 'liquor';

-- Home
UPDATE categories SET icon = 'cart-shopping'      WHERE slug = 'groceries';
UPDATE categories SET icon = 'house'              WHERE slug = 'rent';
UPDATE categories SET icon = 'building-columns'   WHERE slug = 'mortgage';
UPDATE categories SET icon = 'plug'               WHERE slug = 'electronics';
UPDATE categories SET icon = 'couch'              WHERE slug = 'furniture';
UPDATE categories SET icon = 'pump-soap'          WHERE slug = 'household_supplies';
UPDATE categories SET icon = 'screwdriver-wrench' WHERE slug = 'maintenance';
UPDATE categories SET icon = 'broom'              WHERE slug = 'cleaning';
UPDATE categories SET icon = 'paw'                WHERE slug = 'pets';
UPDATE categories SET icon = 'bell-concierge'     WHERE slug = 'services';

-- Life
UPDATE categories SET icon = 'baby'                WHERE slug = 'childcare';
UPDATE categories SET icon = 'shirt'               WHERE slug = 'clothing';
UPDATE categories SET icon = 'graduation-cap'      WHERE slug = 'education';
UPDATE categories SET icon = 'gift'                WHERE slug = 'gifts';
UPDATE categories SET icon = 'shield-halved'       WHERE slug = 'insurance';
UPDATE categories SET icon = 'briefcase-medical'   WHERE slug = 'medical';
UPDATE categories SET icon = 'receipt'             WHERE slug = 'taxes';
UPDATE categories SET icon = 'hand-holding-dollar' WHERE slug = 'loan';
UPDATE categories SET icon = 'hotel'               WHERE slug = 'hotel';
UPDATE categories SET icon = 'scale-balanced'      WHERE slug = 'legal';
UPDATE categories SET icon = 'building'            WHERE slug = 'real_estate';

-- Transport
UPDATE categories SET icon = 'bicycle'        WHERE slug = 'bicycle';
UPDATE categories SET icon = 'bus'            WHERE slug = 'bus';
UPDATE categories SET icon = 'car'            WHERE slug = 'car';
UPDATE categories SET icon = 'gas-pump'       WHERE slug = 'fuel';
UPDATE categories SET icon = 'square-parking' WHERE slug = 'parking';
UPDATE categories SET icon = 'plane'          WHERE slug = 'plane';
UPDATE categories SET icon = 'taxi'           WHERE slug = 'taxi';
UPDATE categories SET icon = 'train'          WHERE slug = 'train';

-- Utilities
UPDATE categories SET icon = 'bolt'    WHERE slug = 'electricity';
UPDATE categories SET icon = 'fire'    WHERE slug = 'heating_gas';
UPDATE categories SET icon = 'wifi'    WHERE slug = 'internet';
UPDATE categories SET icon = 'phone'   WHERE slug = 'phone';
UPDATE categories SET icon = 'trash'   WHERE slug = 'trash';
UPDATE categories SET icon = 'tv'      WHERE slug = 'tv';
UPDATE categories SET icon = 'droplet' WHERE slug = 'water';

-- No category
UPDATE categories SET icon = 'thumbtack' WHERE slug = 'other';

ALTER TABLE categories ALTER COLUMN icon SET NOT NULL;
