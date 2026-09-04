-- 對應技術架構設計第2節 orders schema + 第5節訂單狀態機、第8節webhook_deliveries最小欄位。

CREATE TABLE master_wallets (
    id                 BIGSERIAL PRIMARY KEY,
    xpub               TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    -- index 0 永遠保留：分配邏輯一律先 +1 再寫回，第一筆訂單拿到 index 1（見CLAUDE.md安全鐵律5）。
    last_derived_index BIGINT NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE orders (
    id                                 BIGSERIAL PRIMARY KEY,
    merchant_order_no                  TEXT NOT NULL UNIQUE,
    public_token                       TEXT NOT NULL UNIQUE,
    master_wallet_id                   BIGINT NOT NULL REFERENCES master_wallets (id),
    derivation_index                   BIGINT NOT NULL,
    address                            TEXT NOT NULL UNIQUE,
    status                             TEXT NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'CONFIRMING', 'COMPLETED', 'OVERPAID', 'CONFIRMATION_STALLED', 'EXPIRED')),
    target_amount                      BIGINT NOT NULL,
    amount_lower_bound                 BIGINT NOT NULL,
    amount_upper_bound                 BIGINT NOT NULL,
    validity_seconds                   BIGINT NOT NULL,
    amount_tolerance_percent           NUMERIC(5, 2) NOT NULL,
    confirmation_stall_timeout_seconds BIGINT NOT NULL,
    expires_at                         TIMESTAMPTZ NOT NULL,
    confirming_at                      TIMESTAMPTZ,
    consolidation_status               TEXT NOT NULL DEFAULT 'not_consolidated'
        CHECK (consolidation_status IN ('not_consolidated', 'consolidated')),
    created_at                         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (master_wallet_id, derivation_index)
);

CREATE TABLE order_state_transitions (
    id                 BIGSERIAL PRIMARY KEY,
    order_id           BIGINT NOT NULL REFERENCES orders (id),
    from_status        TEXT NOT NULL,
    to_status          TEXT NOT NULL,
    changed_by         TEXT NOT NULL,
    note               TEXT,
    decision_snapshot  JSONB,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_order_state_transitions_order_id ON order_state_transitions (order_id);

CREATE TABLE incoming_transactions (
    id           BIGSERIAL PRIMARY KEY,
    order_id     BIGINT NOT NULL REFERENCES orders (id),
    tx_hash      TEXT NOT NULL,
    log_index    INTEGER NOT NULL,
    amount       BIGINT NOT NULL,
    confirmed    BOOLEAN NOT NULL DEFAULT false,
    block_number BIGINT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tx_hash, log_index)
);

CREATE INDEX idx_incoming_transactions_order_id ON incoming_transactions (order_id);

-- 第8節webhook模組尚未完整實作（worker/重試留待internal/webhook模組），這裡先建最小可用schema，
-- 讓模組5「終態轉換時同交易INSERT一筆」的骨架現在就能接上。
CREATE TABLE webhook_deliveries (
    id             BIGSERIAL PRIMARY KEY,
    event_id       TEXT NOT NULL UNIQUE,
    order_id       BIGINT NOT NULL REFERENCES orders (id),
    event_type     TEXT NOT NULL,
    payload        TEXT NOT NULL,
    status         TEXT NOT NULL
        CHECK (status IN ('awaiting_config', 'pending', 'sending', 'delivered', 'failed')),
    attempt_count  INTEGER NOT NULL DEFAULT 0,
    next_retry_at  TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_deliveries_order_id ON webhook_deliveries (order_id);

-- 單列設定表（第2節），本次只用得到webhook_url/webhook_secret（決定終態INSERT的初始status，見第8節
-- 「未設定URL/secret的邊界」）；其餘參數欄位留待internal/admin參數設定頁模組時再加。
CREATE TABLE system_params (
    id             SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    webhook_url    TEXT,
    webhook_secret TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO system_params (id) VALUES (1);
