-- 對應技術架構設計第4節「鏈上監控模組」。scan_checkpoint是單列表，記錄internal/scanner
-- 兩個獨立掃描迴圈（任務2 only_confirmed=false、任務3 only_confirmed=true）各自的進度。
-- 值以TronGrid的block_timestamp（ms epoch）為單位存放時間戳watermark，不用區塊高度——
-- 查詢用的是min_block_timestamp參數，用時間戳記可以直接比對、不需要區塊高度↔時間戳的
-- 額外轉換。第一列由internal/scanner.EnsureCheckpoint在應用程式啟動時種入，不在migration
-- 裡預先INSERT（種子值必須是啟動當下的時間，寫死在SQL沒有意義）。
CREATE TABLE scan_checkpoint (
    id                                    SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_synced_block_timestamp          BIGINT NOT NULL,
    last_synced_at                       TIMESTAMPTZ,
    last_finality_synced_block_timestamp BIGINT NOT NULL,
    last_finality_synced_at              TIMESTAMPTZ,
    updated_at                            TIMESTAMPTZ NOT NULL DEFAULT now()
);
