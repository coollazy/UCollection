-- 對應需求書5.9「手續費來源地址簿」（v0.39）與技術架構設計第10節（v0.17）：
-- 與歸集地址簿（consolidation_address_book）為兩份獨立表，結構相同、分開管理。
CREATE TABLE fee_source_address_book (
    id         BIGSERIAL PRIMARY KEY,
    address    TEXT NOT NULL UNIQUE,
    label      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
