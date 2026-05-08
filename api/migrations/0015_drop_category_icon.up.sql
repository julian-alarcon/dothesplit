-- Drop the icon column from categories.
--
-- Icon choice is a presentation concern (same as the per-group colors which
-- already live in CSS, not the DB). Categories are a closed, developer-
-- controlled set, so the slug -> icon mapping is owned by the frontend at
-- web/src/lib/category-icons.ts. This keeps the API surface thin and lets
-- us swap icon libraries (FA -> Lucide, etc.) without touching SQL.

ALTER TABLE categories DROP COLUMN icon;
