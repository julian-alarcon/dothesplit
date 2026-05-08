-- Group categories under section headers in the picker (Entertainment,
-- Food & drink, Home, Life, Transport, Utilities, No category).
--
-- Drops the now-redundant umbrella categories from migration 0005 since
-- migration 0012 already provides granular alternatives. Adds a few extra
-- Entertainment items (books, concerts, hobbies, theater) and reslots every
-- surviving row's sort number into dense per-group ranges so the existing
-- ORDER BY sort, label keeps each group contiguous.
--
-- expenses.category_id and recurring_expenses.category_id reference
-- categories(id) with default NO ACTION (see migration 0005), so any rows
-- pointing at the umbrellas must be reassigned before the DELETE.

ALTER TABLE categories ADD COLUMN group_label TEXT;

-- Reassign references on the soon-to-be-deleted umbrella slugs to 'other'.
UPDATE expenses SET category_id = (SELECT id FROM categories WHERE slug = 'other')
 WHERE category_id IN (SELECT id FROM categories WHERE slug IN
   ('food_drink','transport','housing','utilities',
    'entertainment','travel','health','shopping'));

UPDATE recurring_expenses SET category_id = (SELECT id FROM categories WHERE slug = 'other')
 WHERE category_id IN (SELECT id FROM categories WHERE slug IN
   ('food_drink','transport','housing','utilities',
    'entertainment','travel','health','shopping'));

DELETE FROM categories WHERE slug IN
  ('food_drink','transport','housing','utilities',
   'entertainment','travel','health','shopping');

-- New items not present in 0005 / 0012.
INSERT INTO categories (slug, label, emoji, sort) VALUES
    ('books',       'Books',       '📚', 110),
    ('concerts',    'Concerts',    '🎤', 120),
    ('hobbies',     'Hobbies',     '🎨', 140),
    ('theater',     'Theater',     '🎭', 180),
    ('snacks',      'Snacks',      '🍿', 240),
    ('legal',       'Legal',       '⚖️', 495),
    ('real_estate', 'Real estate', '🏘️', 498);

-- Reslot every surviving row.
-- Entertainment (1xx)
UPDATE categories SET sort = 130, group_label = 'Entertainment' WHERE slug = 'games';
UPDATE categories SET sort = 150, group_label = 'Entertainment' WHERE slug = 'movies';
UPDATE categories SET sort = 160, group_label = 'Entertainment' WHERE slug = 'music';
UPDATE categories SET sort = 170, group_label = 'Entertainment' WHERE slug = 'sports';
UPDATE categories SET                       group_label = 'Entertainment' WHERE slug IN ('books','concerts','hobbies','theater');

-- Food & drink (2xx). Snacks sits between groceries-style staples and out-of-home.
UPDATE categories SET sort = 220, group_label = 'Food & drink' WHERE slug = 'snacks';
UPDATE categories SET sort = 230, group_label = 'Food & drink' WHERE slug = 'dining_out';
UPDATE categories SET sort = 240, group_label = 'Food & drink' WHERE slug = 'liquor';

-- Home (3xx). Groceries lives here too: it's recurring household consumables.
UPDATE categories SET sort = 305, group_label = 'Home' WHERE slug = 'groceries';
UPDATE categories SET sort = 310, group_label = 'Home' WHERE slug = 'rent';
UPDATE categories SET sort = 320, group_label = 'Home' WHERE slug = 'mortgage';
UPDATE categories SET sort = 330, group_label = 'Home' WHERE slug = 'electronics';
UPDATE categories SET sort = 340, group_label = 'Home' WHERE slug = 'furniture';
UPDATE categories SET sort = 350, group_label = 'Home' WHERE slug = 'household_supplies';
UPDATE categories SET sort = 360, group_label = 'Home' WHERE slug = 'maintenance';
UPDATE categories SET sort = 370, group_label = 'Home' WHERE slug = 'cleaning';
UPDATE categories SET sort = 380, group_label = 'Home' WHERE slug = 'pets';
UPDATE categories SET sort = 390, group_label = 'Home' WHERE slug = 'services';

-- Life (4xx)
UPDATE categories SET sort = 410, group_label = 'Life' WHERE slug = 'childcare';
UPDATE categories SET sort = 420, group_label = 'Life' WHERE slug = 'clothing';
UPDATE categories SET sort = 430, group_label = 'Life' WHERE slug = 'education';
UPDATE categories SET sort = 440, group_label = 'Life' WHERE slug = 'gifts';
UPDATE categories SET sort = 450, group_label = 'Life' WHERE slug = 'insurance';
UPDATE categories SET sort = 460, group_label = 'Life' WHERE slug = 'medical';
UPDATE categories SET sort = 470, group_label = 'Life' WHERE slug = 'taxes';
UPDATE categories SET sort = 480, group_label = 'Life' WHERE slug = 'loan';
UPDATE categories SET sort = 490, group_label = 'Life' WHERE slug = 'hotel';
UPDATE categories SET                       group_label = 'Life' WHERE slug IN ('legal','real_estate');

-- Transport (5xx)
UPDATE categories SET sort = 510, group_label = 'Transport' WHERE slug = 'bicycle';
UPDATE categories SET sort = 520, group_label = 'Transport' WHERE slug = 'bus';
UPDATE categories SET sort = 530, group_label = 'Transport' WHERE slug = 'car';
UPDATE categories SET sort = 540, group_label = 'Transport' WHERE slug = 'fuel';
UPDATE categories SET sort = 550, group_label = 'Transport' WHERE slug = 'parking';
UPDATE categories SET sort = 560, group_label = 'Transport' WHERE slug = 'plane';
UPDATE categories SET sort = 570, group_label = 'Transport' WHERE slug = 'taxi';
UPDATE categories SET sort = 580, group_label = 'Transport' WHERE slug = 'train';

-- Utilities (6xx)
UPDATE categories SET sort = 610, group_label = 'Utilities' WHERE slug = 'electricity';
UPDATE categories SET sort = 620, group_label = 'Utilities' WHERE slug = 'heating_gas';
UPDATE categories SET sort = 630, group_label = 'Utilities' WHERE slug = 'internet';
UPDATE categories SET sort = 640, group_label = 'Utilities' WHERE slug = 'phone';
UPDATE categories SET sort = 650, group_label = 'Utilities' WHERE slug = 'trash';
UPDATE categories SET sort = 660, group_label = 'Utilities' WHERE slug = 'tv';
UPDATE categories SET sort = 670, group_label = 'Utilities' WHERE slug = 'water';

-- No category (9xx). 'other' keeps its label; the group header carries the
-- "No category" copy so the single item inside doesn't read redundantly.
UPDATE categories SET sort = 999, group_label = 'No category' WHERE slug = 'other';

ALTER TABLE categories ALTER COLUMN group_label SET NOT NULL;
