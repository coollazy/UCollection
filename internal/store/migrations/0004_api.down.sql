DROP TABLE IF EXISTS api_keys;

ALTER TABLE system_params
    DROP COLUMN IF EXISTS validity_seconds,
    DROP COLUMN IF EXISTS amount_tolerance_percent,
    DROP COLUMN IF EXISTS confirmation_stall_timeout_seconds;
