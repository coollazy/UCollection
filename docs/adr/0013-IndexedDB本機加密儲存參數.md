# ADR-0013：IndexedDB 本機加密儲存參數（PBKDF2 600,000 次）

- Status: Accepted
- Date: 2026-09-02

## 背景

需求書5.9允許商戶把手動歸集用的助記詞以自訂密碼加密存於瀏覽器 IndexedDB，省得每次操作都要重新輸入。這是階段01完整度審查發現的缺口（M5），對應階段02驗證項目7驗證 PBKDF2+AES-GCM（瀏覽器原生 WebCrypto，不需第三方函式庫）加解密助記詞的正確性，驗證用的迭代次數是300,000（僅為測試參數，非正式安全值）。

## 決策

- **KDF**：PBKDF2，SHA-256，鹽16 bytes（`crypto.getRandomValues`），**迭代次數正式拍板 600,000**。
- **加密**：AES-GCM，256 bits，IV 12 bytes（每次加密重新產生）。
- **索引鍵**：`master_wallet_id`（系統內部穩定主鍵，非 xpub 字串本身，但語意等同需求書「以代收主錢包識別碼/xpub當索引鍵」）——不同代收主錢包各自獨立一筆 IndexedDB 記錄，互不覆蓋。

## 理由

- 600,000 次依 OWASP Password Storage Cheat Sheet 對 PBKDF2-HMAC-SHA256 的建議下限直接定案，屬純技術安全參數、不涉及業務判斷。
- 認證標籤驗證失敗（密碼錯誤）直接觸發 `OperationError`，不會靜默解出錯誤內容——已由驗證項目7在真實頁面 reload 模擬跨 session 讀取確認此行為。
- `crypto.subtle`/`indexedDB` 為瀏覽器原生API，已驗證不受嚴格CSP `script-src 'self'` 影響，沿用同一頁面規格。

## 已知限制（需求書已定案，不是本決策的疏漏）

IndexedDB 在清除瀏覽器資料/無痕模式/換裝置時會遺失。UX設計需明確告知商戶這只是「省得每次輸入」的便利功能，不能取代助記詞本身的備份責任。

## 相關文件

- [驗證結論-07-本機加密儲存IndexedDB.md](../驗證結論-07-本機加密儲存IndexedDB.md)
- [技術架構設計.md 第10節「本機加密儲存」](../開發流程框架-03-技術架構設計.md#10-手動歸集模組internalconsolidation)
- [技術架構設計變更紀錄 v0.11](../開發流程框架-03-技術架構設計變更紀錄.md#v0-11)
