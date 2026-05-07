ALTER TABLE users ADD COLUMN week_start SMALLINT NOT NULL DEFAULT 1;
-- 0 = Sunday, 1 = Monday. Default Monday for EU bias.
