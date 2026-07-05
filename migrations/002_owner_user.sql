-- Owner/admin identity seed.
-- Apply with:
--   psql "$DATABASE_URL" -v owner_password='...' -f migrations/002_owner_user.sql

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS slug TEXT,
    ADD COLUMN IF NOT EXISTS lifetime_access BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS access_expires_at TIMESTAMPTZ;

DROP INDEX IF EXISTS idx_tenants_slug;
CREATE UNIQUE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug);

CREATE TABLE IF NOT EXISTS users (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID REFERENCES tenants(id) ON DELETE SET NULL,
    email             TEXT NOT NULL,
    email_normalized  TEXT GENERATED ALWAYS AS (lower(email)) STORED,
    password_hash     TEXT NOT NULL,
    full_name         TEXT NOT NULL,
    role              TEXT NOT NULL CHECK (role IN ('owner','admin','member')),
    plan              TEXT NOT NULL CHECK (plan IN ('free','starter','pro','enterprise')) DEFAULT 'free',
    lifetime_access   BOOLEAN NOT NULL DEFAULT false,
    access_expires_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_normalized ON users (email_normalized);
CREATE INDEX IF NOT EXISTS idx_users_tenant ON users (tenant_id);

DO $$
DECLARE
    owner_password TEXT := current_setting('pixelaudit.owner_password', true);
    owner_tenant_id UUID;
BEGIN
    IF owner_password IS NULL OR owner_password = '' THEN
        RAISE EXCEPTION 'Missing pixelaudit.owner_password. Run with PGOPTIONS="-c pixelaudit.owner_password=..." or set it before applying this migration.';
    END IF;

    INSERT INTO tenants (name, plan, api_key_hash, weights, rate_limit_rpm, slug, lifetime_access, access_expires_at)
    VALUES (
        'PixelAudit Owner',
        'enterprise',
        digest('owner-local-dev-key', 'sha256'),
        '{}'::jsonb,
        100000,
        'pixelaudit-owner',
        true,
        NULL
    )
    ON CONFLICT (slug) DO UPDATE SET
        plan = 'enterprise',
        lifetime_access = true,
        access_expires_at = NULL,
        rate_limit_rpm = GREATEST(tenants.rate_limit_rpm, 100000)
    RETURNING id INTO owner_tenant_id;

    INSERT INTO users (
        tenant_id,
        email,
        password_hash,
        full_name,
        role,
        plan,
        lifetime_access,
        access_expires_at
    )
    VALUES (
        owner_tenant_id,
        'paulo@pixelaudit.com',
        crypt(owner_password, gen_salt('bf', 12)),
        'Paulo',
        'owner',
        'enterprise',
        true,
        NULL
    )
    ON CONFLICT (email_normalized) DO UPDATE SET
        tenant_id = EXCLUDED.tenant_id,
        password_hash = EXCLUDED.password_hash,
        full_name = EXCLUDED.full_name,
        role = 'owner',
        plan = 'enterprise',
        lifetime_access = true,
        access_expires_at = NULL,
        updated_at = now();
END $$;
