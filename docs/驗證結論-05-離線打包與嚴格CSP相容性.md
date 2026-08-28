# 驗證結論：驗證項目 5（xpub 衍生函式庫離線打包 + 嚴格 CSP 相容性）

- 對應：[技術可行性驗證](技術可行性驗證.md) 驗證項目 5
- 驗證日期：2026-08-28
- 驗證用程式碼位置：`/private/tmp/claude-501/-Users-user-Documents-Codes-UCollection/796e4128-2eb2-4af5-b87b-2e96dc604ffa/scratchpad/browser-verify/`（暫存目錄，未進正式專案結構，可丟棄；與驗證項目 4 共用同一份打包產物）
- 測試瀏覽器：Chrome 151.0.0.0（macOS，透過 `claude-in-chrome` 遠端操控真實瀏覽器分頁執行）

## 打包方式

延續驗證項目 4 選定的函式庫組合（`bip39`+`hdkey`+`secp256k1` 路線 A、`@scure/bip39`+`@scure/bip32`+`@noble/curves` 路線 B，見 [驗證結論-04](驗證結論-04-瀏覽器端xpub衍生.md) 詳細版本），用 `esbuild` 0.28.2 `--bundle --platform=browser` 打包成兩個靜態檔案：

- `polyfill.bundle.js`（`Buffer`/`process` 等 Node.js 全域變數 polyfill）
- `verify.bundle.js`（實際的助記詞→xpub→地址衍生邏輯，含兩條路線的所有依賴）

頁面本身（`index.html` + `runner.js`）只用 `<script src="./xxx.js">` 引入上述兩個本地檔案，**不引入任何 CDN 連結、不使用 inline `<script>` 區塊**（inline script 在嚴格 CSP `script-src 'self'` 下會被擋，因此改成外部 `runner.js` 檔案，這本身也更貼近正式系統的做法）。

## 套用的嚴格 CSP 規則

用一個簡易 HTTP server（`http.server.SimpleHTTPRequestHandler` 加上自訂 `end_headers()`）在**回應標頭**（而非 `<meta>` 標籤，更貼近正式環境用 nginx/後端框架設定 HTTP header 的做法）加上：

```
Content-Security-Policy: default-src 'none'; script-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'
```

重點限制：`script-src 'self'` 不含 `unsafe-eval`、不含 `unsafe-inline`；`default-src 'none'` 表示未明確列出的資源類型一律禁止；`connect-src 'self'` 限制頁面只能對同源發出網路請求（若函式庫內部偷偷呼叫外部 API 會被擋下並在 console 出現違規訊息）。

## 驗證結果

1. **離線打包**：頁面載入時的所有網路請求（透過瀏覽器實際攔截記錄）：

   | # | URL | 結果 |
   |---|---|---|
   | 1 | `http://localhost:8935/index.html` | 200 |
   | 2 | `http://localhost:8935/polyfill.bundle.js` | 200 |
   | 3 | `http://localhost:8935/verify.bundle.js` | 200 |
   | 4 | `http://localhost:8935/runner.js` | 200 |
   | 5 | `http://localhost:8935/favicon.ico` | 404（瀏覽器自動請求，非頁面程式碼觸發，不影響功能） |

   全部請求皆為同源（`localhost:8935`），沒有任何外部網域的請求，確認函式庫已完全離線打包、不依賴 CDN 或任何第三方網路資源。

2. **嚴格 CSP 下的衍生運算結果**：與驗證項目 4（無 CSP 限制環境）逐項比對，完全一致——
   - 路線 A、B 算出的 xpub 皆為 `xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaQgmv8D45oNUKx6DZMNZBCd`，與驗證結論-01/03 一致。
   - index 0～4 衍生地址與驗證結論-01 逐一一致。
   - 隨機產生的新助記詞（本次為 `arrest photo decline short option rude plate dice gauge supply hurry ostrich`）格式合法，路線 A、B 交叉核對的 xpub 一致。

3. **CSP violation 檢查**：用 `read_console_messages` 以 `Refused|Content Security Policy|violat` 為關鍵字過濾整段 console 紀錄，**沒有找到任何 CSP 違規訊息**；正常執行的 log（`VERIFY_RESULT_JSON_START/END` 與結果 JSON）皆正常輸出，代表 `hdkey`/`secp256k1`（elliptic 後備實作）/`@scure/*`/`@noble/*`/`bs58check`/`buffer`/`crypto-browserify` 這整組依賴鏈在執行過程中，沒有任何一個套件觸發 `eval`、`new Function()` 或其他會被 `script-src 'self'`（不含 `unsafe-eval`）擋下的動態程式碼執行機制。

## 通過條件檢查

- [x] 函式庫離線打包成功，頁面不依賴任何外部網路請求即可完整運作（5 個請求全數同源，唯一非 200 的是瀏覽器自動請求的 favicon，不影響功能）。
- [x] 在套用嚴格 CSP 的情況下，衍生運算結果仍然正確（與驗證項目 4 一致，xpub、5 個地址、隨機助記詞交叉核對全部相符）。
- [x] 瀏覽器 console 沒有任何 CSP violation 錯誤。

## 過程中的觀察 / 沒有預期到的狀況

- 原本用 `<meta http-equiv="Content-Security-Policy">` 起手測試，但頁面內原本有一段 inline `<script>` 負責呼叫衍生函式並輸出結果，在 `script-src 'self'` 下必然被擋（inline script 沒有 `'unsafe-inline'` 就是會被擋，這是預期中的行為，不是函式庫的問題）。改成外部 `runner.js` 檔案後即正常運作。**這是本次驗證中唯一「原本會失敗」的狀況，但屬於頁面本身的寫法問題，不是函式庫相容性問題**——正式開發時，任何送去這種高風險頁面的程式碼本來就不該用 inline script，剛好被這次驗證提前攔到。
- 為了更貼近正式部署情境，改用 HTTP 回應標頭而非 `<meta>` 標籤設定 CSP（`<meta>` 標籤版的 CSP 有一些限制，例如不支援 `frame-ancestors`），兩種方式對本次驗證的函式庫執行結果沒有差異，但正式系統建議統一用伺服器端 HTTP header 設定，涵蓋範圍更完整。
- 除了 inline script 這個頁面寫法問題外，函式庫本身（`hdkey`、`secp256k1`/`elliptic`、`@scure/bip39`、`@scure/bip32`、`@noble/curves`、`@noble/hashes`、`bs58check`、`buffer`、`crypto-browserify`）在嚴格 CSP 下沒有踩到任何衝突，不需要放寬 CSP 規則或更換函式庫。

## 最終判定：**PASS**

三項通過條件全部符合。驗證項目 4 選定的函式庫組合，經實際離線打包（不連外網、不經 CDN）並套用嚴格 CSP（`script-src 'self'`，無 `unsafe-eval`/`unsafe-inline`）後，衍生運算結果與驗證項目 4 完全一致，且未觸發任何 CSP 違規。需求書 5.1／5.9 對「不引入第三方腳本/CDN、函式庫離線打包、套用嚴格 CSP」的要求，在這組函式庫下技術上可行，可以作為階段 03 架構設計的依據。

## 對階段 03 的提醒

- 高風險頁面（xpub 取得頁面、手動歸集頁面）的前端程式碼**不可使用 inline `<script>`**，所有邏輯需寫在外部 `.js` 檔案並用 `<script src="...">` 引入，這是本次驗證中唯一需要調整寫法才能符合嚴格 CSP 的地方。
- CSP 建議在正式系統中透過伺服器端（Go 後端或前方 nginx）統一設定 HTTP 回應標頭，而非 `<meta>` 標籤，涵蓋範圍更完整、也更難被頁面上的其他程式碼意外覆蓋。
- 本次驗證用的 CSP 規則（`default-src 'none'; script-src 'self'; connect-src 'self'; ...`）可直接作為正式高風險頁面 CSP 設定的起始版本，架構設計階段可視實際頁面需求（例如是否需要載入自身圖片/字型）微調 `img-src`/`font-src` 等其餘欄位，但 `script-src` 不含 `unsafe-eval`/`unsafe-inline` 這條建議維持不變。
