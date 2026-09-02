# CLAUDE.md

UCollection：單租戶自架的 USDT-TRC20 代收軟體。商戶自行部署，系統依 xpub 為每筆訂單衍生獨立收款地址，掃描 Tron 鏈上事件判斷入帳，並提供後台管理與手動歸集。

本檔案放**不可協商的規則**——寫死的架構限制、安全鐵律、業務鐵律。「為什麼選這個方案」的完整討論與被否決的替代方案見 `docs/adr/`；模組層級的完整設計見 `docs/開發流程框架-03-技術架構設計.md`；需求範圍見 `docs/開發流程框架-01-需求書.md`；目前進度見 `docs/進度.md`。**改動下面任何一條規則之前，先確認對應的 ADR 是否也需要一併修訂（見「與 ADR 的關係」）。**

## 技術棧（見 [ADR-0001](docs/adr/0001-語言與執行環境選型.md)/[0002](docs/adr/0002-資料庫選型.md)/[0003](docs/adr/0003-前端技術選型.md)）

- **語言**：Go。單一 binary，內部以常駐 goroutine 執行鏈上監控背景任務，不拆分多 process、不引入 message queue。
- **資料庫**：PostgreSQL。地址分配用 `pg_advisory_xact_lock` 處理併發，不要改用其他鎖機制。
- **前端**：Go `html/template` + htmx，零 Node.js 建置鏈。僅高風險頁面（見下方）例外，用 `esbuild` 離線打包單獨 vendor。
- **部署**：docker-compose 雙 container（`app` + `db`），不內建反向代理/TLS 終止（見 [ADR-0004](docs/adr/0004-不內建反向代理.md)）。

## Package 劃分（見技術架構設計第1節）

```
cmd/ucollection/          — 入口，啟動HTTP server + 背景scanner goroutine
internal/config/          — 環境變數設定
internal/store/           — DB存取層（含migrations）
internal/hdwallet/        — xpub衍生地址邏輯
internal/order/           — 訂單/狀態機
internal/scanner/         — 鏈上事件掃描背景任務
internal/tronclient/      — 封裝呼叫TronGrid（scanner/tron-proxy/admin reverify共用）
internal/webhook/         — 推送/重試
internal/api/             — 對外API handler
internal/admin/           — 後台handler
internal/checkout/        — 前台handler
internal/consolidation/   — 手動歸集後端邏輯
internal/auth/            — 帳號/2FA/session
internal/audit/           — 審計日誌
web/static/js/            — 離線打包的助記詞衍生/簽名JS bundle
```

新增功能前先確認該放進哪個既有 package，不要在既有邊界之外另開新的頂層目錄。

## 安全鐵律（違反等於引入資金風險，改動前必須先跟人類確認）

1. **伺服器全程不接觸、不儲存私鑰或助記詞**。任何私鑰衍生、簽名動作只能發生在瀏覽器本地。後端只處理 xpub（公鑰，非機密）。
2. **組交易與廣播一律經後端 `/tron-proxy` 代理**，瀏覽器不可直接呼叫 TronGrid。這是 CSP `connect-src 'self'` 能夠成立的前提，見 [驗證結論-08](docs/驗證結論-08-嚴格CSP下組交易簽名廣播.md)。
3. **處理助記詞/私鑰輸入的頁面**（代收主錢包設定、手動歸集簽名頁、TRX手續費頁）套用嚴格 CSP（`script-src 'self'`，無 `unsafe-eval`/`unsafe-inline`，無 inline `<script>`），衍生/簽名函式庫離線打包隨系統部署，不引入任何第三方腳本/CDN/分析工具。
4. **簽名後、廣播前必做「本地重算 txID 核對」與「反推地址自我核對」**，不符則中止，不可省略（見技術架構設計第10節「組交易與廣播」）。
5. **`index 0` 永遠保留，不分配給任何訂單、不作系統內部用途**。任何需要一個「代收主錢包底下的具體地址」的情境（例如 TRX 手續費來源）一律要求商戶另外指定地址，不可挪用 index 0。
6. **金額全程用 `int64` 最小單位整數運算，禁止用浮點數比較或儲存金額**。
7. **`broadcasttransaction` 網路層失敗不等於交易未發生**，不可直接標記失敗後重新廣播；要先查鏈上狀態（`gettransactioninfobyid`）才能判定，避免重複轉出。
8. **API Key 的 `secret` 與 Webhook 的 `webhook_secret` 明碼存資料庫是刻意決策**（HMAC 驗簽需要伺服器重算，雜湊值無法逆推），不要把它「修正」成雜湊存——那會讓 HMAC 機制整個失效。反之，session token 與 API Key 本身的 `key` 一律只存 hash，不存明碼，僅在產生當下的 HTTP 回應中明碼回傳一次。
9. **`/tron-proxy/...`、手動改判、代收主錢包切換、API Key 重新產生、Webhook URL/secret 變更，一律套用 `RequireFreshTOTP(15分鐘)`**，不能只掛一般登入 session。判準見技術架構設計第11節「權限分級理由」——凡是攻擊者僅取得 session cookie（未取得2FA裝置）就能造成資金劫持、身分冒用、或誤導商戶財務判斷的操作，都屬此類。新增後台寫入端點時，先用這個判準檢查是否也該疊加。

## 業務鐵律

1. **地址與訂單永久一對一綁定**，即使訂單終態也不可回收重新分配。
2. **不使用商戶可調的「確認數」概念**。訂單是否達最終確定，一律直接沿用 TronGrid `only_confirmed=true` 這個布林結果（Tron 協定自身的 solidified 狀態），不要加回自訂確認數/緩衝期機制（見 [ADR-0006](docs/adr/0006-confirmations-required改版.md)，這條規則曾經存在又被拿掉，不要復原）。
3. **主要監控必須用區塊/事件掃描**（`GET /v1/contracts/{USDT合約}/events`），**不可逐一輪詢地址列表**。備援/降級情境（事件掃描持續失敗時）例外，範圍限定當下 PENDING/CONFIRMING 訂單，不適用於主要監控。
4. **訂單不提供取消/作廢功能**，僅能經有效期到期自然轉 EXPIRED。
5. **系統不主動判斷改判方向**（CONFIRMATION_STALLED/EXPIRED 是否該改判為 COMPLETED/OVERPAID），一律由商戶人工查證後決定。手動改判路徑刻意不重查金額子查詢——這是設計，不是漏洞。
6. **訂單曾經歷過的狀態不因後續改判覆蓋或刪除**，`order_state_transitions` 是 append-only 的完整歷史。
7. **已歸集地址（`consolidation_status='consolidated'`）永久不再監控**，之後若該地址又收到新入帳，系統不主動偵測/通知——這是需求書已定案的已知限制，不要「順手」加上重新監控當作改善。

## 何時要停下來問人類，不要自己決定

- 修改任何涉及金鑰衍生、簽名、廣播判斷邏輯的程式碼——這類 bug「錯了會默默壞掉、不會報錯」，屬框架定義的高風險等級，方案要先核准、diff 要逐行看過，不能只憑測試通過。
- 任何會讓「安全鐵律」清單變動的改動。
- 需要新增/移除某個 `RequireFreshTOTP` 保護時——判斷邏輯要能重新推導出「攻擊者僅取得 session cookie 能造成什麼後果」，想不清楚就先問。
- 與需求書/技術架構設計/ADR 現有內容矛盾的改動，即使看起來是「順手改進」。

## 與 ADR 的關係

上面的規則是結論，ADR（`docs/adr/`）是完整的理由與被否決的替代方案。**改動任何一條規則前，先讀對應的 ADR**——多數規則背後都有一段「為什麼不選看起來更直覺的方案」的討論（例如 index 0 保留、API Key 明碼存、確認數改版），沒讀過就改動很容易把已經想清楚、否決過的方案原封不動重新提出來。若確實要推翻某個 ADR 的結論，新增一份「取代」它的 ADR，不要直接刪改舊的。
