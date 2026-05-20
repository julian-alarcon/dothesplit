-- Email verification + transactional mailer.
--
-- - users.email_verified_at: null while a registration is unverified; non-null
--   after the user submits the 6-digit code (or auto-set when SMTP was
--   unconfigured at register time so the bootstrap admin isn't stuck).
-- - users.notification_prefs: per-event email opt-in flags. JSONB so new keys
--   can be added without further migrations. Absent key means "off".
-- - email_verification_tokens: SHA-256 of a 6-digit code. Purpose discriminates
--   register vs change_email vs password_reset (last is for the future). For
--   change_email we cache the prospective new email's hash + ciphertext until
--   confirm so the user keeps logging in with the old address until it's done.
-- - email_outbox: outbound queue drained by the worker every minute. Stores
--   the recipient email AES-GCM-encrypted (same EmailCipher as users.email)
--   so plaintext addresses never sit at rest in this table.
ALTER TABLE users
    ADD COLUMN email_verified_at  TIMESTAMPTZ,
    ADD COLUMN notification_prefs JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE email_verification_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose         TEXT NOT NULL CHECK (purpose IN ('register','change_email','password_reset')),
    code_hash       BYTEA NOT NULL,
    new_email_hash  BYTEA,
    new_email_enc   BYTEA,
    attempts        SMALLINT NOT NULL DEFAULT 0,
    expires_at      TIMESTAMPTZ NOT NULL,
    consumed_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_evt_user_purpose_active
    ON email_verification_tokens (user_id, purpose)
    WHERE consumed_at IS NULL;

CREATE TABLE email_outbox (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    to_email_enc    BYTEA NOT NULL,
    subject         TEXT NOT NULL,
    body            TEXT NOT NULL,
    template        TEXT NOT NULL,
    attempts        SMALLINT NOT NULL DEFAULT 0,
    last_error      TEXT,
    sent_at         TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_outbox_pending
    ON email_outbox (next_attempt_at)
    WHERE sent_at IS NULL AND attempts < 5;
