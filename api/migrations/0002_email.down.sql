DROP TABLE IF EXISTS email_outbox;
DROP TABLE IF EXISTS email_verification_tokens;
ALTER TABLE users DROP COLUMN IF EXISTS notification_prefs;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
