-- key_hint語意見CLAUDE.md安全鐵律8、docs/adr/0019-API-Key片段供後台識別.md：
-- key本身只存hash（key_hash），額外存前4+後4碼明碼片段供後台列表識別用途，僅供
-- 識別、不足以重建金鑰。nullable：本次上線前已存在的舊記錄無法回溯回填。
ALTER TABLE api_keys ADD COLUMN key_hint TEXT;
