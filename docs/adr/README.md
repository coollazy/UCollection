# 決策紀錄（ADR）

記錄 UCollection 每個重大架構決策「為什麼選這個方案、否決過哪些替代方案」。目的是避免脈絡只存在對話紀錄裡，之後被新的開發 session 悄悄翻案、重新提出已經想清楚並否決過的方案。

- 這裡放的是**決策本身與理由**，精簡、面向未來讀者。完整的討論過程、逐輪修正細節見 [開發流程框架-03-技術架構設計變更紀錄.md](../開發流程框架-03-技術架構設計變更紀錄.md)。
- **ADR 一旦 Accepted 就不回頭改內容**（除非發現記錄有誤）。之後若決策被推翻，新增一份「取代」它的 ADR，並在被取代的那份加註 `Status: Superseded by ADR-00XX`，不要直接刪改舊的——這正是 ADR 存在的意義：留下決策當下為什麼這樣選的完整脈絡。
- 不可協商的規則摘要見專案根目錄 [CLAUDE.md](../../CLAUDE.md)；模組層級完整設計見 [開發流程框架-03-技術架構設計.md](../開發流程框架-03-技術架構設計.md)。

## 索引

| 編號 | 標題 | 狀態 |
|---|---|---|
| [0001](0001-語言與執行環境選型.md) | 語言與執行環境選型：Go 單一 binary | Accepted |
| [0002](0002-資料庫選型.md) | 資料庫選型：PostgreSQL | Accepted |
| [0003](0003-前端技術選型.md) | 前端技術選型：html/template + htmx，零 Node.js 建置鏈 | Accepted |
| [0004](0004-不內建反向代理.md) | 不內建反向代理/TLS 終止 | Accepted |
| [0005](0005-代收主錢包取得方式.md) | 代收主錢包（xpub）取得方式 | Accepted |
| [0006](0006-confirmations-required改版.md) | 拿掉商戶可調「確認數」，改固定沿用 Tron 協定 solidified 狀態 | Accepted |
| [0007](0007-鏈上監控事件掃描優先於輪詢.md) | 主要監控用事件掃描，不逐一輪詢地址 | Accepted |
| [0008](0008-checkpoint安全緩衝值.md) | checkpoint 安全緩衝 N = 20 個區塊 | Accepted |
| [0009](0009-組交易廣播由後端代理.md) | 組交易與廣播統一由後端 `/tron-proxy` 代理 | Accepted |
| [0010](0010-API-Key與Webhook-secret明碼存.md) | API Key 與 Webhook secret 明碼存資料庫 | Accepted |
| [0011](0011-兩階段登入與step-up驗證.md) | 密碼+TOTP 兩階段登入與 RequireFreshTOTP step-up 驗證 | Accepted |
| [0012](0012-TRX手續費A1A2機制.md) | TRX 手續費 A1/A2：不動用 index 0，改採商戶自訂地址 | Accepted |
| [0013](0013-IndexedDB本機加密儲存參數.md) | IndexedDB 本機加密儲存參數（PBKDF2 600,000 次） | Accepted |
| [0014](0014-手動改判疊加TOTP.md) | 後台手動改判疊加 RequireFreshTOTP | Accepted |
| [0015](0015-已歸集地址晚到入帳可見度.md) | 已歸集地址晚到入帳改為可見度標示（推翻v0.24舊決策） | Accepted |
| [0016](0016-高風險操作每次要求TOTP不設新鮮度窗口.md) | 高風險操作每次要求TOTP驗證碼，取消15分鐘新鮮度窗口（推翻ADR-0011部分決策） | Accepted |
| [0017](0017-手動歸集流程重構自動編排與精省手續費.md) | 手動歸集流程整合為單一流程、系統自動編排與精省手續費 | Accepted |
