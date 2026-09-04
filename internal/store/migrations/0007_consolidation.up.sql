-- 對應技術架構設計第10節「手動歸集模組」Part 1（後端邏輯）：歸集地址簿、USDT歸集
-- 批次/明細、TRX手續費批次/明細。
CREATE TABLE consolidation_address_book (
    id         BIGSERIAL PRIMARY KEY,
    address    TEXT NOT NULL UNIQUE,
    label      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE consolidation_batches (
    id                   BIGSERIAL PRIMARY KEY,
    master_wallet_id     BIGINT NOT NULL REFERENCES master_wallets (id),
    destination_address  TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- amount/tx_hash是NOT NULL：這張表的列只在prepare當下才建立（見broadcast_status
-- 狀態機定義，'pending'本身就代表「已prepare」），不是批次建立時預先插入的空列。
CREATE TABLE consolidation_items (
    id                BIGSERIAL PRIMARY KEY,
    batch_id          BIGINT NOT NULL REFERENCES consolidation_batches (id),
    order_id          BIGINT NOT NULL REFERENCES orders (id),
    amount            BIGINT NOT NULL,
    tx_hash           TEXT NOT NULL,
    broadcast_status  TEXT NOT NULL DEFAULT 'pending'
        CHECK (broadcast_status IN ('pending', 'broadcasting', 'success', 'failed')),
    error_detail      TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_consolidation_items_batch_id ON consolidation_items (batch_id);
CREATE INDEX idx_consolidation_items_order_id_created_at ON consolidation_items (order_id, created_at DESC);

CREATE TABLE fee_topup_batches (
    id                  BIGSERIAL PRIMARY KEY,
    master_wallet_id    BIGINT NOT NULL REFERENCES master_wallets (id),
    fee_source          TEXT NOT NULL CHECK (fee_source IN ('A1', 'A2')),
    fee_source_address  TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE fee_topup_items (
    id                BIGSERIAL PRIMARY KEY,
    batch_id          BIGINT NOT NULL REFERENCES fee_topup_batches (id),
    order_id          BIGINT NOT NULL REFERENCES orders (id),
    amount            BIGINT NOT NULL,
    tx_hash           TEXT NOT NULL,
    broadcast_status  TEXT NOT NULL DEFAULT 'pending'
        CHECK (broadcast_status IN ('pending', 'broadcasting', 'success', 'failed')),
    error_detail      TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_fee_topup_items_batch_id ON fee_topup_items (batch_id);
CREATE INDEX idx_fee_topup_items_order_id_created_at ON fee_topup_items (order_id, created_at DESC);
