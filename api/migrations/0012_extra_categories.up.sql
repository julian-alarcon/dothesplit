-- Expand the category list with specific items alongside the existing
-- high-level ones. Existing rows (groceries, food_drink, transport, housing,
-- utilities, entertainment, travel, health, shopping, other) keep their
-- ids untouched, so referencing expenses and recurring templates aren't
-- affected.
--
-- Sort numbers grouped: 1xx leisure, 2xx food, 3xx home, 4xx housing,
-- 5xx personal/finance, 6xx transport, 7xx utilities. "Other" stays at 100
-- for backwards compatibility but the new entries fall in around the existing
-- ones so the picker reads in a sensible order.

INSERT INTO categories (slug, label, emoji, sort) VALUES
    -- Leisure
    ('games',             'Games',             '🎮', 110),
    ('movies',            'Movies',            '🎬', 120),
    ('music',             'Music',             '🎵', 130),
    ('sports',            'Sports',            '🏟️', 140),
    -- Food (groceries already exists at sort 10)
    ('dining_out',        'Dining out',        '🍽️', 210),
    ('liquor',            'Liquor',            '🍷', 220),
    -- Home goods
    ('electronics',       'Electronics',       '📱', 310),
    ('furniture',         'Furniture',         '🛋️', 320),
    ('household_supplies','Household supplies','🧴', 330),
    ('maintenance',       'Maintenance',       '🔧', 340),
    -- Housing
    ('mortgage',          'Mortgage',          '🏦', 410),
    ('rent',              'Rent',              '🏠', 420),
    ('pets',              'Pets',              '🐾', 430),
    ('services',          'Services',          '🛎️', 440),
    -- Personal & finance
    ('childcare',         'Childcare',         '🧒', 510),
    ('clothing',          'Clothing',          '👕', 520),
    ('education',         'Education',         '🎓', 530),
    ('gifts',             'Gifts',             '🎁', 540),
    ('insurance',         'Insurance',         '🛡️', 550),
    ('medical',           'Medical expenses',  '💊', 560),
    ('taxes',             'Taxes',             '🧾', 570),
    ('loan',              'Loan',              '💰', 580),
    -- Transport
    ('bicycle',           'Bicycle',           '🚲', 610),
    ('train',             'Train',             '🚆', 620),
    ('bus',               'Bus',               '🚌', 630),
    ('car',               'Car',               '🚗', 640),
    ('fuel',              'Gas / Fuel',        '⛽', 650),
    ('hotel',             'Hotel',             '🏨', 660),
    ('parking',           'Parking',           '🅿️', 670),
    ('plane',             'Plane',             '✈️', 680),
    ('taxi',              'Taxi',              '🚕', 690),
    -- Utilities
    ('cleaning',          'Cleaning',          '🧹', 710),
    ('electricity',       'Electricity',       '💡', 720),
    ('heating_gas',       'Heating / Gas',     '🔥', 730),
    ('trash',             'Trash',             '🗑️', 740),
    ('tv',                'TV',                '📺', 750),
    ('internet',          'Internet',          '🌐', 760),
    ('phone',             'Phone',             '📞', 770),
    ('water',             'Water',             '🚰', 780);
