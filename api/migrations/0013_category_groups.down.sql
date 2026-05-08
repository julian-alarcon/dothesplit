-- Roll back 0013_category_groups: remove the group_label column, drop the
-- newly-added Entertainment items, restore the 8 umbrella categories with
-- their original sort values, and put surviving 0012 rows back at the sort
-- numbers they had right after 0012.
--
-- Reassigned expense / recurring_expense references on 'other' are NOT
-- restored (the originals are gone), matching 0012's down migration which
-- already accepts that categories carry historical data.

-- Drop group_label first so the INSERTs below don't have to supply it
-- (the column is NOT NULL once 0013 finishes applying).
ALTER TABLE categories DROP COLUMN group_label;

DELETE FROM categories WHERE slug IN
  ('books','concerts','hobbies','theater','snacks','legal','real_estate');

INSERT INTO categories (slug, label, emoji, sort) VALUES
    ('food_drink',    'Food & Drink',  '🍽️', 20),
    ('transport',     'Transport',     '🚗', 30),
    ('housing',       'Housing',       '🏠', 40),
    ('utilities',     'Utilities',     '💡', 50),
    ('entertainment', 'Entertainment', '🎬', 60),
    ('travel',        'Travel',        '✈️', 70),
    ('health',        'Health',        '💊', 80),
    ('shopping',      'Shopping',      '🛍️', 90);

-- Restore originals' sort numbers.
UPDATE categories SET sort = 10  WHERE slug = 'groceries';
UPDATE categories SET sort = 100 WHERE slug = 'other';

-- Restore 0012 sort numbers.
UPDATE categories SET sort = 110 WHERE slug = 'games';
UPDATE categories SET sort = 120 WHERE slug = 'movies';
UPDATE categories SET sort = 130 WHERE slug = 'music';
UPDATE categories SET sort = 140 WHERE slug = 'sports';
UPDATE categories SET sort = 210 WHERE slug = 'dining_out';
UPDATE categories SET sort = 220 WHERE slug = 'liquor';
UPDATE categories SET sort = 310 WHERE slug = 'electronics';
UPDATE categories SET sort = 320 WHERE slug = 'furniture';
UPDATE categories SET sort = 330 WHERE slug = 'household_supplies';
UPDATE categories SET sort = 340 WHERE slug = 'maintenance';
UPDATE categories SET sort = 410 WHERE slug = 'mortgage';
UPDATE categories SET sort = 420 WHERE slug = 'rent';
UPDATE categories SET sort = 430 WHERE slug = 'pets';
UPDATE categories SET sort = 440 WHERE slug = 'services';
UPDATE categories SET sort = 510 WHERE slug = 'childcare';
UPDATE categories SET sort = 520 WHERE slug = 'clothing';
UPDATE categories SET sort = 530 WHERE slug = 'education';
UPDATE categories SET sort = 540 WHERE slug = 'gifts';
UPDATE categories SET sort = 550 WHERE slug = 'insurance';
UPDATE categories SET sort = 560 WHERE slug = 'medical';
UPDATE categories SET sort = 570 WHERE slug = 'taxes';
UPDATE categories SET sort = 580 WHERE slug = 'loan';
UPDATE categories SET sort = 610 WHERE slug = 'bicycle';
UPDATE categories SET sort = 620 WHERE slug = 'train';
UPDATE categories SET sort = 630 WHERE slug = 'bus';
UPDATE categories SET sort = 640 WHERE slug = 'car';
UPDATE categories SET sort = 650 WHERE slug = 'fuel';
UPDATE categories SET sort = 660 WHERE slug = 'hotel';
UPDATE categories SET sort = 670 WHERE slug = 'parking';
UPDATE categories SET sort = 680 WHERE slug = 'plane';
UPDATE categories SET sort = 690 WHERE slug = 'taxi';
UPDATE categories SET sort = 710 WHERE slug = 'cleaning';
UPDATE categories SET sort = 720 WHERE slug = 'electricity';
UPDATE categories SET sort = 730 WHERE slug = 'heating_gas';
UPDATE categories SET sort = 740 WHERE slug = 'trash';
UPDATE categories SET sort = 750 WHERE slug = 'tv';
UPDATE categories SET sort = 760 WHERE slug = 'internet';
UPDATE categories SET sort = 770 WHERE slug = 'phone';
UPDATE categories SET sort = 780 WHERE slug = 'water';
