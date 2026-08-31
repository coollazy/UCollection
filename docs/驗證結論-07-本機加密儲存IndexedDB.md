# 驗證結論：驗證項目 7（本機加密儲存，助記詞加密存入 IndexedDB）

- 對應：[開發流程框架-02-技術可行性驗證.md](開發流程框架-02-技術可行性驗證.md) 驗證項目 7
- 驗證日期：2026-08-31
- 驗證用程式碼位置：`/private/tmp/claude-501/-Users-user-Documents-Codes-UCollection/b4d86345-0c88-4579-8201-f45168e20cc9/scratchpad/verify7/`（暫存目錄，未進正式專案結構，可丟棄；xpub/地址交叉核對邏輯重用自驗證項目 4/5 的既有 bundle，來源見下方）
- 測試瀏覽器：Chrome 151.0.0.0（macOS，透過 `claude-in-chrome` 遠端操控真實瀏覽器分頁執行，非模擬環境）

## 使用的技術與參數

全程使用瀏覽器**原生** WebCrypto API（`window.crypto.subtle`）與 IndexedDB（`window.indexedDB`），未引入任何第三方加密函式庫：

| 項目 | 選用 |
|---|---|
| 金鑰衍生（KDF） | `PBKDF2`，雜湊 `SHA-256`，迭代次數 `300000`，鹽值 16 bytes（`crypto.getRandomValues` 隨機產生） |
| 加密演算法 | `AES-GCM`，金鑰長度 256 bits，IV 12 bytes（隨機產生，每次加密皆重新產生） |
| 儲存 | IndexedDB，資料庫 `ucollection_verify7`，物件儲存區 `wallet`，記錄含 `salt`、`iv`、`ciphertext`、`iterations` |

> 迭代次數 `300000` 為本次驗證測試參數，僅供證明流程可行，非正式系統的最終建議值；正式參數應在階段 03/04 依當時 OWASP 建議與實際裝置效能權衡另定。

xpub/地址交叉核對邏輯（步驟 5）重用驗證項目 4/5 既有的路線 A（`bip39` + `hdkey`，見 [驗證結論-04](驗證結論-04-瀏覽器端xpub衍生.md)），改寫成參數化函式（可傳入任意助記詞字串），讓「從 IndexedDB 解密還原出的助記詞」能直接餵入既驗證過的衍生管線，而非只跟寫死的常數比對。

## 測試助記詞來源

沿用驗證項目 1、3、4、5 完全相同的公開 BIP39 標準測試助記詞：
`abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about`

## 驗證流程與結果

測試頁面依「IndexedDB 是否已有記錄」自動切換兩個階段，用**真實的瀏覽器頁面重新整理**（非同一次 JS 記憶體狀態延續）模擬跨 session：

### Phase 1（加密並存入）

首次載入頁面，用測試密碼（`Test-Password-2026!`）PBKDF2 衍生金鑰，AES-GCM 加密測試助記詞，寫入 IndexedDB。回傳：

```json
{
  "phase": "setup",
  "status": "stored",
  "pbkdf2Iterations": 300000
}
```

### Phase 2（真實 reload 後，跨 session 讀取＋解密＋交叉核對）

重新整理頁面（新的 JS 執行環境，IndexedDB 資料為磁碟持久化讀出，非記憶體延續）：

| 檢查項 | 結果 |
|---|---|
| IndexedDB 讀出記錄含完整 salt(16 bytes)/iv(12 bytes)/ciphertext | ✅ `hasSalt: true, hasIv: true, ciphertextLength: 109` |
| 正確密碼解密成功 | ✅ `succeeded: true` |
| 解密還原助記詞與原文逐字一致 | ✅ `matchesOriginal: true` |
| 還原助記詞重新衍生 xpub | `xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaQgmv8D45oNUKx6DZMNZBCd` |
| 與驗證結論-01/03/04 記錄的 xpub 比對 | ✅ 完全一致 |
| 還原助記詞重新衍生 index 0～4 地址 | `TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH`、`TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK`、`TYJPRrdB5APNeRs4R7fYZSwW3TcrTKw2gx`、`TRhVWK5XEDkQBDevcdCWW7RW51aRncty4W`、`TT2X2yyubp7qpAWYYNE5JQWBtoZ7ikQFsY` |
| 與驗證結論-01/03/04 記錄的 5 個地址逐一比對 | ✅ 完全一致（5/5） |

### 反向測試：錯誤密碼解密

用同一筆 IndexedDB 記錄，改用錯誤密碼（`Wrong-Password-2026!`）嘗試解密：

```json
{
  "wrongPasswordTest": {
    "threwError": true,
    "errorName": "OperationError",
    "errorMessage": "OperationError"
  }
}
```

AES-GCM 內建的認證標籤（authentication tag）驗證機制正常運作——錯誤金鑰**直接丟出例外**（`OperationError`），沒有靜默解出一段亂碼當作「解密成功」。這代表就算日後系統邏輯疏漏、忘記另外檢查解密結果是否為合法助記詞，錯誤密碼這條路徑本身就不會安靜地放行。

### 嚴格 CSP 相容性

沿用驗證項目 5 同等級的嚴格 CSP（透過 HTTP 回應標頭，而非 `<meta>` 標籤）：

```
Content-Security-Policy: default-src 'none'; script-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'
```

檢查結果：

- 頁面全部網路請求（`index.html`、`polyfill.bundle.js`、`crossderive.bundle.js`、`webcrypto-store.js`、`runner.js`）皆為同源 `localhost:8936`，無任何外部請求。
- 用 `read_console_messages` 以 `Refused|Content Security Policy|violat` 過濾三次獨立頁面 reload（含 Phase 1、Phase 2、額外重整）的完整 console 紀錄，**沒有找到任何 CSP violation**。`crypto.subtle`（PBKDF2/AES-GCM）與 `indexedDB` 相關呼叫全程正常執行，未被 `script-src 'self'`（無 `unsafe-eval`/`unsafe-inline`）擋下——這符合預期，WebCrypto/IndexedDB 是瀏覽器原生 API，不涉及動態程式碼執行，本來就不該與嚴格 CSP 衝突。

## 通過條件檢查

- [x] 加密後存入 IndexedDB、跨 session 讀出、正確密碼解密，還原助記詞與原文完全一致。
- [x] 還原助記詞衍生出的 xpub/地址與驗證結論-01/03/04 逐一相同。
- [x] 錯誤密碼解密明確失敗（`AES-GCM` 認證標籤驗證觸發 `OperationError`，不會靜默解出錯誤內容）。
- [x] 嚴格 CSP 下，WebCrypto／IndexedDB 相關呼叫皆無 violation。

## 過程中的觀察 / 沒有預期到的狀況

- 「跨 session」刻意採用**真實的頁面 reload**（`navigate` 到同一 URL）而非用程式手動清變數模擬，因為 JS 記憶體內的變數清除不能代表 IndexedDB 真的是從磁碟持久層重新讀出——這點延續驗證項目 5「用 HTTP header 而非 meta 標籤設 CSP，更貼近正式環境」的同一個原則：驗證步驟要盡量對應真實情境，不能因為程式碼上寫得到就假設等價。
- 交叉核對 xpub/地址時，沒有重新從零刻一套衍生邏輯，而是把驗證項目 4 既有、已交叉驗證過的路線 A（`bip39`+`hdkey`）改寫成參數化函式（原本 `runAll()` 只驗證寫死的常數）。這樣「解密還原出的助記詞」是真的當作參數傳入衍生管線重新算一次，而不是只做字串比對後assume衍生結果理所當然一致，讓這個關鍵橋接檢查更站得住腳。
- 其餘沒有出現版本落差或行為跟預期不符的狀況；PBKDF2/AES-GCM/IndexedDB 這三個瀏覽器原生 API 組合起來一次到位，沒有踩到任何相容性或邏輯坑。

## 最終判定：**PASS**

四項通過條件全部符合。瀏覽器原生 WebCrypto（PBKDF2 + AES-GCM）與 IndexedDB 足以完成需求書 5.9「本機加密儲存」的技術需求：助記詞可正確加密存入、跨 session 正確解密還原（逐字一致）、還原結果與既有衍生驗證完全收斂、錯誤密碼會明確失敗不會靜默誤導、且在需求書要求的嚴格 CSP 下無相容性問題。**不需要引入任何第三方加密函式庫**，可作為階段 03 架構設計中「本機加密儲存」功能的技術依據。

## 對階段 03 的提醒

- PBKDF2 迭代次數（本次測試用 300000）、是否改用 `Argon2`等更強 KDF（WebCrypto 原生不支援 Argon2，需第三方 WASM 實作，會牴觸「不引入第三方腳本」的原則）等正式參數，屬於架構設計階段的安全性權衡，本次驗證只確認「技術路徑可行」，未做參數調優或效能基準測試。
- 本次驗證的「跨 session」只驗證同一瀏覽器、同一裝置重新整理頁面的情境。IndexedDB 資料在**清除瀏覽器資料**、**無痕模式**、或**更換裝置**時會遺失或不可用，這是 IndexedDB 儲存機制本身的限制，非本次驗證範圍，但架構設計時商戶端 UX 需要明確告知這個限制（例如提示商戶另外備份助記詞，本機加密儲存只是「省得每次輸入」的便利功能，不能取代助記詞本身的備份責任）。
- 錯誤密碼會拋出 `OperationError`（瀏覽器標準 `DOMException` 名稱），架構設計 UI 邏輯時可直接依此例外類型判斷「密碼錯誤」並提示商戶重新輸入，不需要額外自訂錯誤碼機制。
- 離線打包與嚴格 CSP 的整體結論與驗證項目 5 一致（外部程式碼一律用 `<script src="...">` 引入、CSP 走 HTTP header），本項目未發現需要另外調整的地方。
