-- 對應技術架構設計第9節「帳號安全模組」：單一管理帳號、後台登入session、通用稽核
-- 日誌。三張表皆為新表（前四個模組不需要，此模組第一個用到）。
CREATE TABLE admin_account (
    id                   BIGSERIAL PRIMARY KEY,
    username             TEXT NOT NULL,
    password_hash        TEXT NOT NULL,
    totp_secret          TEXT,   -- NULL代表尚未完成2FA設定，見第9節步驟2
    last_totp_step       BIGINT, -- TOTP重放防護，見第9節「TOTP驗證參數」
    password_changed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- 單一租戶只會有一筆，由internal/auth.EnsureAdminAccount在啟動時以
-- 「COUNT為0才INSERT」保證，不用CHECK(id=1)這種寫法（username由環境變數決定，
-- 非system_params那種固定id語意的單列設定表）。

CREATE TABLE admin_sessions (
    id                     BIGSERIAL PRIMARY KEY,
    token_hash             TEXT NOT NULL UNIQUE, -- SHA-256 hex，cookie存原始token
    status                 TEXT NOT NULL CHECK (status IN ('pending_2fa', 'active')),
    pending_totp_secret    TEXT,    -- 僅首次設定2FA流程使用，見第9節
    totp_attempt_count     INTEGER NOT NULL DEFAULT 0,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at             TIMESTAMPTZ NOT NULL,
    last_seen_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_totp_verified_at  TIMESTAMPTZ
);

CREATE INDEX idx_admin_sessions_expires_at ON admin_sessions (expires_at);

CREATE TABLE audit_logs (
    id          BIGSERIAL PRIMARY KEY,
    actor       TEXT NOT NULL,
    action_type TEXT NOT NULL,
    target_type TEXT,
    target_id   BIGINT,
    detail      JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_action_type_created_at ON audit_logs (action_type, created_at);
