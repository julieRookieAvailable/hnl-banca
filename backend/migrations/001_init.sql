-- 001_init.sql
-- Banca en Línea HNL: esquema inicial.
-- TigerBeetle es la fuente de verdad del dinero; Postgres guarda metadatos.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Usuarios
CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    full_name     TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Cuentas bancarias (metadatos; el balance vive en TigerBeetle)
CREATE TABLE IF NOT EXISTS bank_accounts (
    account_number TEXT PRIMARY KEY,
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tb_account_id  BIGINT NOT NULL UNIQUE,
    account_type   TEXT NOT NULL CHECK (account_type IN ('checking', 'savings', 'investment')),
    currency       TEXT NOT NULL DEFAULT 'USD',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_bank_accounts_user_id ON bank_accounts(user_id);

-- Tokens de refresco
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id          BIGSERIAL PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);

-- Historial de transacciones (metadatos legibles; el movimiento vive en TigerBeetle)
CREATE TABLE IF NOT EXISTS transactions (
    id             BIGSERIAL PRIMARY KEY,
    from_account   TEXT,
    to_account     TEXT,
    type           TEXT NOT NULL CHECK (type IN ('deposit', 'withdrawal', 'transfer', 'internal_transfer')),
    amount         NUMERIC(18, 2) NOT NULL CHECK (amount > 0),
    description    TEXT NOT NULL DEFAULT '',
    timestamp      TIMESTAMPTZ NOT NULL,
    status         TEXT NOT NULL DEFAULT 'completed',
    tb_transfer_id BIGINT
);

CREATE INDEX IF NOT EXISTS idx_transactions_from_account ON transactions(from_account);
CREATE INDEX IF NOT EXISTS idx_transactions_to_account ON transactions(to_account);
CREATE INDEX IF NOT EXISTS idx_transactions_timestamp ON transactions(timestamp);

-- Transferencias pendientes creadas por el asistente (dos fases: pending -> confirm/void)
CREATE TABLE IF NOT EXISTS pending_transfers (
    id            BIGSERIAL PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tb_pending_id TEXT NOT NULL UNIQUE,
    from_account  TEXT NOT NULL,
    to_account    TEXT NOT NULL,
    amount_cents  BIGINT NOT NULL CHECK (amount_cents > 0),
    description   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'voided', 'expired')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_pending_transfers_user_id ON pending_transfers(user_id);

-- Idempotency keys: guardan la respuesta de operaciones de dinero para devolver
-- exactamente el mismo resultado ante reintentos con la misma clave.
CREATE TABLE IF NOT EXISTS idempotency_keys (
    id             BIGSERIAL PRIMARY KEY,
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL,
    response       JSONB NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, idempotency_key)
);
