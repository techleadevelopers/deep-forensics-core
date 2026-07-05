-- PixelAudit — schema inicial

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS tenants (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    plan           TEXT NOT NULL CHECK (plan IN ('free','starter','pro','enterprise')) DEFAULT 'free',
    api_key_hash   BYTEA NOT NULL,
    weights        JSONB NOT NULL DEFAULT '{}'::jsonb,
    rate_limit_rpm INT  NOT NULL DEFAULT 60,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS verifications (
    id                  TEXT PRIMARY KEY,
    tenant_id           TEXT,
    sha256              BYTEA,
    phash               BIGINT,
    s3_key              TEXT,
    heatmap_s3_key      TEXT,
    status              TEXT NOT NULL CHECK (status IN ('pending','completed','failed')),
    authentic           BOOLEAN,
    confidence          NUMERIC(6,2),
    recommendation      TEXT,
    priority            TEXT,
    analysis            JSONB,
    model_versions      JSONB,
    processing_time_ms  INT,
    order_id            TEXT,
    metadata            JSONB,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_verif_tenant_created ON verifications(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_verif_sha256         ON verifications(sha256);
CREATE INDEX IF NOT EXISTS idx_verif_status         ON verifications(status);

CREATE TABLE IF NOT EXISTS webhooks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    verification_id TEXT REFERENCES verifications(id) ON DELETE CASCADE,
    url             TEXT NOT NULL,
    status          TEXT NOT NULL,
    attempts        INT  NOT NULL DEFAULT 0,
    last_attempt    TIMESTAMPTZ,
    next_attempt    TIMESTAMPTZ,
    response_code   INT,
    response_body   TEXT
);

CREATE TABLE IF NOT EXISTS usage_events (
    tenant_id UUID   NOT NULL,
    day       DATE   NOT NULL,
    count     BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, day)
);
