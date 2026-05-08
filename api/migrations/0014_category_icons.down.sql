-- Roll back 0014: rename icon column back to emoji and restore each row's
-- emoji glyph from migrations 0005, 0012, 0013.

ALTER TABLE categories RENAME COLUMN icon TO emoji;

-- Originals from 0005
UPDATE categories SET emoji = '🛒'  WHERE slug = 'groceries';
UPDATE categories SET emoji = '📌'  WHERE slug = 'other';

-- 0012
UPDATE categories SET emoji = '🎮'  WHERE slug = 'games';
UPDATE categories SET emoji = '🎬'  WHERE slug = 'movies';
UPDATE categories SET emoji = '🎵'  WHERE slug = 'music';
UPDATE categories SET emoji = '🏟️'  WHERE slug = 'sports';
UPDATE categories SET emoji = '🍽️'  WHERE slug = 'dining_out';
UPDATE categories SET emoji = '🍷'  WHERE slug = 'liquor';
UPDATE categories SET emoji = '📱'  WHERE slug = 'electronics';
UPDATE categories SET emoji = '🛋️'  WHERE slug = 'furniture';
UPDATE categories SET emoji = '🧴'  WHERE slug = 'household_supplies';
UPDATE categories SET emoji = '🔧'  WHERE slug = 'maintenance';
UPDATE categories SET emoji = '🏦'  WHERE slug = 'mortgage';
UPDATE categories SET emoji = '🏠'  WHERE slug = 'rent';
UPDATE categories SET emoji = '🐾'  WHERE slug = 'pets';
UPDATE categories SET emoji = '🛎️'  WHERE slug = 'services';
UPDATE categories SET emoji = '🧒'  WHERE slug = 'childcare';
UPDATE categories SET emoji = '👕'  WHERE slug = 'clothing';
UPDATE categories SET emoji = '🎓'  WHERE slug = 'education';
UPDATE categories SET emoji = '🎁'  WHERE slug = 'gifts';
UPDATE categories SET emoji = '🛡️'  WHERE slug = 'insurance';
UPDATE categories SET emoji = '💊'  WHERE slug = 'medical';
UPDATE categories SET emoji = '🧾'  WHERE slug = 'taxes';
UPDATE categories SET emoji = '💰'  WHERE slug = 'loan';
UPDATE categories SET emoji = '🚲'  WHERE slug = 'bicycle';
UPDATE categories SET emoji = '🚆'  WHERE slug = 'train';
UPDATE categories SET emoji = '🚌'  WHERE slug = 'bus';
UPDATE categories SET emoji = '🚗'  WHERE slug = 'car';
UPDATE categories SET emoji = '⛽'  WHERE slug = 'fuel';
UPDATE categories SET emoji = '🏨'  WHERE slug = 'hotel';
UPDATE categories SET emoji = '🅿️'  WHERE slug = 'parking';
UPDATE categories SET emoji = '✈️'  WHERE slug = 'plane';
UPDATE categories SET emoji = '🚕'  WHERE slug = 'taxi';
UPDATE categories SET emoji = '🧹'  WHERE slug = 'cleaning';
UPDATE categories SET emoji = '💡'  WHERE slug = 'electricity';
UPDATE categories SET emoji = '🔥'  WHERE slug = 'heating_gas';
UPDATE categories SET emoji = '🗑️'  WHERE slug = 'trash';
UPDATE categories SET emoji = '📺'  WHERE slug = 'tv';
UPDATE categories SET emoji = '🌐'  WHERE slug = 'internet';
UPDATE categories SET emoji = '📞'  WHERE slug = 'phone';
UPDATE categories SET emoji = '🚰'  WHERE slug = 'water';

-- 0013 additions
UPDATE categories SET emoji = '📚'  WHERE slug = 'books';
UPDATE categories SET emoji = '🎤'  WHERE slug = 'concerts';
UPDATE categories SET emoji = '🎨'  WHERE slug = 'hobbies';
UPDATE categories SET emoji = '🎭'  WHERE slug = 'theater';
UPDATE categories SET emoji = '🍿'  WHERE slug = 'snacks';
UPDATE categories SET emoji = '⚖️'  WHERE slug = 'legal';
UPDATE categories SET emoji = '🏘️'  WHERE slug = 'real_estate';
