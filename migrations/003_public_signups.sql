CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS public_signups (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email                 TEXT NOT NULL,
    email_normalized      TEXT GENERATED ALWAYS AS (lower(email)) STORED,
    full_name             TEXT NOT NULL,
    company               TEXT,
    source                TEXT NOT NULL DEFAULT 'upload_image',
    welcome_email_sent_at TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_public_signups_email_normalized
    ON public_signups (email_normalized);
