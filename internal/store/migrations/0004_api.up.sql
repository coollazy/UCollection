-- 對應技術架構設計第7節「對外API模組」。system_params補上internal/order那次刻意
-- 先留白的三個訂單建立參數（validity_seconds/amount_tolerance_percent/
-- confirmation_stall_timeout_seconds）——nullable，因為需求書只講「商戶可設全域
-- 預設值」，沒定義開箱即用的內建數字；商戶尚未透過admin參數設定頁設定前，
-- internal/api的建單handler遇到NULL會回傳明確錯誤，不是自行編一個預設值。
ALTER TABLE system_params
    ADD COLUMN validity_seconds BIGINT,
    ADD COLUMN amount_tolerance_percent NUMERIC(5, 2),
    ADD COLUMN confirmation_stall_timeout_seconds BIGINT;

-- key_hash/secret語意見CLAUDE.md安全鐵律8：key本身只存hash，secret明碼存
-- （HMAC驗簽需要伺服器重算）。
CREATE TABLE api_keys (
    id         BIGSERIAL PRIMARY KEY,
    key_hash   TEXT NOT NULL UNIQUE,
    secret     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);
