# 驗證結論：驗證項目 8（手動歸集「組交易＋簽名＋廣播」程式碼在嚴格 CSP 下的相容性）

- 對應：[開發流程框架-02-技術可行性驗證.md](開發流程框架-02-技術可行性驗證.md) 驗證項目 8
- 驗證日期：2026-09-01
- 使用的測試網：Shasta
- 測試瀏覽器：Chrome 151.0.0.0（macOS，透過 `claude-in-chrome` 遠端操控真實瀏覽器分頁執行）
- 驗證用程式碼位置：`/private/tmp/claude-501/-Users-user-Documents-Codes-UCollection/b4d86345-0c88-4579-8201-f45168e20cc9/scratchpad/verify8/`（暫存目錄，未進正式專案結構，可丟棄；衍生/簽名邏輯直接沿用驗證項目 6 既有程式碼，未重寫）

## 背景緣由

完整度回頭審查發現：驗證項目 6（組交易＋簽名＋廣播）從未套用過驗證項目 5/7 要求的嚴格 CSP，也從未檢查 console 有無 CSP violation；驗證用的 `fetch` 更是直接打去外部 TronGrid 網域，跟需求書 5.9 步驟 6「瀏覽器只送簽名結果給自己後端、後端負責廣播」的架構不同。但需求書 5.9「輸入助記詞頁面的強化要求」明文要求歸集簽名頁面（含 TRX 手續費 A1/A2 頁面）套用同等級嚴格 CSP。本項目補這個缺口。

## 架構調整：同源代理

沿用驗證項目 6 完全相同的衍生/簽名程式碼（`bip39`+`hdkey` 衍生私鑰、`@noble/curves` 手刻 ECDSA 簽名、`prehash:false`、簽名反推地址自我核對），**程式碼本身一行未改**——這正是本項目要驗證的對象。唯一的架構調整：把原本直接打外部 `https://api.shasta.trongrid.io` 的 `fetch` 呼叫，改為打同源路徑 `/tron-proxy/...`，由一個本機 Python HTTP server（`csp_proxy_server.py`）在瀏覽器之外（不受頁面 CSP 限制）代為轉發給 TronGrid：

```
瀏覽器（嚴格CSP頁面）--fetch(同源 /tron-proxy/...)--> Python server --urllib.request--> TronGrid
```

這貼近需求書 5.9 步驟 6 規定的「瀏覽器只跟自己後端溝通、後端負責對外呼叫第三方 API」架構，也讓 CSP 的 `connect-src 'self'` 限制能夠真正套用（而不必為了讓瀏覽器打外部 API 而放寬這條規則）。

## 套用的嚴格 CSP 規則

與驗證項目 5/7 相同等級，透過 HTTP 回應標頭設定：

```
Content-Security-Policy: default-src 'none'; script-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'
```

## 驗證過程

### 第一、二次執行：轉帳金額設定問題（非 CSP/簽名問題）

前兩次執行分別因為「轉帳金額超過付款方餘額」（`SafeMath: subtraction overflow`）與「TRX 餘額不足支付能量費用」（`OUT_OF_ENERGY`）而 REVERT/FAILED，皆與 CSP 或簽名邏輯無關，純粹是測試參數問題。已請使用者透過 `https://shasta.tronex.io/join/getJoinPage` 幫付款方地址（`TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH`）補充 2000 test TRX 後，調整轉帳金額為餘額範圍內的 `88888`（0.088888 USDT）重新執行。

### 第三次執行：成功

| 檢查項 | 結果 |
|---|---|
| 瀏覽器衍生私鑰/地址與驗證結論-03/06 一致 | ✅ |
| 組交易（經同源代理呼叫 `triggersmartcontract`）成功 | ✅ |
| 本地重算 `SHA256(raw_data_hex)` 與伺服器回傳 `txID` 一致 | ✅ |
| 本地簽名完成（`@noble/curves` 手刻 ECDSA，嚴格 CSP 下執行） | ✅ |
| 簽名反推地址與付款方地址一致（廣播前自我核對） | ✅ |
| 廣播（經同源代理）成功，取得 txID `98cbb038f003708ee458cc3b3493719bf61caf3da6711633de4ba41b89dc65d9` | ✅ |
| 鏈上查詢執行結果 | `receipt.result: "SUCCESS"` |
| 頁面回報餘額變化 | `fromDelta: 88888, toDelta: 88888`，與送出金額一致 |

### 獨立 `curl` 查詢（不透過瀏覽器頁面本身的程式碼，比照驗證項目 6 的核對方式）

```
gettransactioninfobyid: receipt.result = "SUCCESS"
Transfer 事件 topic0: ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef（標準 Transfer(address,address,uint256) 簽名）
Transfer 事件 from（hex）: c8599111f29c1e1e061265b4af93ea1f274ad78a —— 對應付款方 TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH ✅
Transfer 事件 to（hex）:   b6e708a39781c96bd399c7657780ff9fe9f052a8 —— 對應收款方 TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK ✅
Transfer 事件金額（data）: 0x15b38 = 88888，與送出金額一致 ✅
付款方 USDT 餘額：1000911112 → 1000822224，差額 -88888 ✅
收款方 USDT 餘額：10792589 → 10881477，差額 +88888 ✅
```

人工核對通過，不只憑瀏覽器頁面自己回報的結果採信。

### CSP violation 檢查

用 `read_console_messages` 讀取完整執行過程的 console（第三次執行，含事先重新整理頁面以確保追蹤已啟動），共 12 則訊息，**全部是驗證程式自己輸出的 `VERIFY8_PROGRESS`/`VERIFY8_RESULT_JSON_*` 記錄，沒有任何 `Refused`/`Content Security Policy`/`violat` 字樣**。

### 網路請求檢查（結構性確認，而非只看有沒有報錯）

用 `read_network_requests` 檢視整段瀏覽器分頁的網路請求紀錄：全部請求（`index.html`、`polyfill.bundle.js`、`bundle.js`、`runner.js`、`/tron-proxy/...` 系列）皆為 `localhost:8936`/`localhost:8938` 同源請求，**沒有任何一筆直接打向外部 TronGrid 網域**——這代表 `connect-src 'self'` 這條限制不是「湊巧沒被違反」，而是架構上根本不存在需要違反它的呼叫，同源代理設計確實成立。

## 通過條件檢查

- [x] 在套用嚴格 CSP（含 `connect-src 'self'`）且所有 TronGrid 呼叫皆改走同源代理的情況下，組交易＋簽名＋廣播整條流程仍可正常執行，功能結果與驗證項目 6 一致（簽名自我核對通過、廣播成功、鏈上查詢執行成功）。
- [x] 廣播成功後，人工核對鏈上實際餘額變化與送出金額一致。
- [x] 瀏覽器 console 沒有任何 CSP violation 錯誤。

## 過程中的觀察 / 沒有預期到的狀況

- 前兩次執行失敗（餘額超額、能量不足）純屬測試參數/測試網資源問題，跟本項目要驗證的 CSP/簽名邏輯無關，如實記錄但不影響最終判定。
- 驗證項目 6 用到的函式庫（`bip39`、`hdkey`、`@noble/curves`、`@noble/hashes`、`bs58check`）全部與驗證項目 5 已測過 CSP 相容性的函式庫組合重疊，這次額外驗證的「新程式碼」實質上只有：手刻 ECDSA 簽名呼叫（`secp256k1.sign()` 特定參數）、手動 ABI 編碼（純字串/位元組運算）、`fetch` 呼叫本身。這幾項全部通過，沒有觸發任何 CSP 限制，與預期一致——但這個「預期」在本項目執行之前只是推測，沒有實測依據，這正是本項目存在的意義。
- 需求書 5.9 步驟 6 沒有明確寫死「組交易」這個動作要在前端還是後端呼叫外部 API，只明確要求「已簽名的交易」送給後端由後端廣播。本項目採用「組交易與廣播都經同源代理」的較保守詮釋（兩者皆不由瀏覽器直接對外呼叫），架構設計階段仍可依實際需求決定是否讓後端額外承擔「組交易」這一步，或改為前端直接向可信的自家後端請求「目前鏈上參數」再自行組裝——不論哪種做法，本項目已證明「瀏覽器端只跟同源溝通＋嚴格 CSP」與「衍生+簽名邏輯」兩者可以共存不衝突。

## 最終判定：**PASS**

三項通過條件全部符合。驗證項目 6 的組交易＋簽名＋廣播程式碼，在套用需求書 5.9 要求的嚴格 CSP、且所有對外部 TronGrid 的呼叫皆改走同源後端代理的架構下，功能結果與驗證項目 6 完全一致，且未觸發任何 CSP 違規、也沒有任何瀏覽器發出的跨網域請求。需求書 5.9「輸入助記詞頁面的強化要求」對歸集簽名頁面（含 TRX 手續費頁面）的 CSP 要求，在此程式碼路徑下技術上可行，缺口已補齊。

## 對階段 03 的提醒

- 手動歸集頁面／TRX 手續費頁面的正式架構，應讓瀏覽器只跟自家後端（同源）溝通，凡是需要對外呼叫 TronGrid 的動作（查詢鏈上狀態組交易、廣播）一律由後端代為執行，不要讓瀏覽器直接連到 TronGrid 網域——這樣才能套用最嚴格的 `connect-src 'self'`，而不必為了遷就直連需求放寬 CSP。
- 驗證項目 6 記錄的地雷（recovery byte 佈局、`prehash` 預設值、廣播網路層失敗不代表交易未發生）在嚴格 CSP 環境下同樣適用，本項目未發現任何 CSP 相關的新地雷，可視為對驗證項目 6 結論的補強，非推翻或修正。
