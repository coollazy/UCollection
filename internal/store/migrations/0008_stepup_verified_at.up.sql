-- ADR-0016「批次寬限」修訂：RequireTOTPCodeOrRecentStepUp（僅4條
-- /tron-proxy/... 端點）需要區分「這個session的新鮮度是因為剛登入」還是「這個
-- session剛為了某個高風險操作明確驗證過一次TOTP碼」。既有的
-- last_totp_verified_at 在登入（activateSession）與step-up（completeReverify）
-- 兩種情境都會更新，如果批次寬限機制沿用它，會變成「剛登入完就能直接呼叫
-- /tron-proxy/...，不需要任何一次明確的step-up驗證」，等同悄悄恢復了ADR-0016
-- 想推翻的舊行為。新增獨立欄位，只在RequireTOTPCode/RequireTOTPCodeOrRecentStepUp
-- 的驗證成功路徑（completeReverify）寫入，登入本身不寫入。
ALTER TABLE admin_sessions ADD COLUMN last_stepup_verified_at TIMESTAMPTZ;
