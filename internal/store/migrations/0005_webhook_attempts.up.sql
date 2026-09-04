-- 對應技術架構設計第8節「Webhook通知模組」：webhook_deliveries每次投遞嘗試的
-- append-only明細，供internal/webhook worker記錄與後台通知歷史查詢用。
CREATE TABLE webhook_delivery_attempts (
    id            BIGSERIAL PRIMARY KEY,
    delivery_id   BIGINT NOT NULL REFERENCES webhook_deliveries (id),
    attempt_no    INTEGER NOT NULL,
    http_status   INTEGER,        -- NULL代表網路層失敗，連HTTP回應都沒收到
    response_body TEXT,           -- 截斷存
    attempted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    triggered_by  TEXT NOT NULL CHECK (triggered_by IN ('auto', 'manual'))
);

CREATE INDEX idx_webhook_delivery_attempts_delivery_id ON webhook_delivery_attempts (delivery_id);
