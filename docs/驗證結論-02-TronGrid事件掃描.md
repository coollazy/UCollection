# 驗證結論：驗證項目 2（TronGrid 測試網事件掃描）

- 對應：[技術可行性驗證](技術可行性驗證.md) 驗證項目 2
- 驗證日期：2026-08-27
- 使用的測試網：Shasta
- 驗證用程式碼位置：`/private/tmp/claude-501/-Users-user-Documents-Codes-UCollection/1aead0a5-6751-4d69-a793-72d85a6f502a/scratchpad/trongrid-verify/`（`gen-account.js`、`gen-target.js`、`check-usdt-balance.js`、`send-transfer.js`、`poll-events.js`，暫存目錄，未進正式專案結構，可丟棄）

## 過程

1. **選網路**：原定 Nile/Shasta 二擇一。Nile 官方水龍頭（nileex.io）與 Shasta 官方水龍頭（shasta.tronex.io）在本次驗證環境下都一度回報「Service is not available in your region」（水龍頭主機的地區限制，非驗證碼或操作問題）；最終由使用者自行用瀏覽器成功在 **Shasta** 水龍頭領到測試幣，改採 Shasta。
2. **測試代幣合約**：沒有另外部署合約——Shasta 水龍頭本身就內建一個現成的 **USDT 測試代幣**（TRC20，合約位址 `TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs`），比自行部署的簡易代幣更貼近正式系統要監控的 USDT 合約，直接沿用。
3. **測試帳號**：本機產生一組純測試用金鑰（`bip32`/`tronweb` 衍生，無真實價值）：
   - 付款方：`TYja3tVY9UkTw2HjH4LV6tUZaJV2GUakLm`
   - 收款方（目標地址）：`TX36g4RCZKJmXio5qpcQAj8Qxpuo9MVX8Z`
4. **領取測試幣**：使用者透過 Shasta 水龍頭網頁，用付款方地址領取 2000 test TRX（付手續費用）與 1000 USDT 測試代幣，領取後鏈上查詢（`wallet/getaccount`、TRC20 `balanceOf`）確認到帳。
5. **發送轉帳**：用 `tronweb` 對 USDT 測試代幣合約呼叫 `transfer()`，從付款方轉帳 **12.345678 USDT**（刻意取一個獨特數字，方便比對辨識）到收款方，記錄廣播完成的時間戳（`2026-08-27T08:25:07.524Z`）與交易雜湊（`223b7a074b2b7085e5aac22a4db54bc322299e2b8e2cbaaa5dd427deaaef05f4`）。
6. **查詢事件**：呼叫 TronGrid Shasta 端點 `GET https://api.shasta.trongrid.io/v1/contracts/{USDT測試合約}/events?event_name=Transfer`，分別用 `only_confirmed=false` 與 `only_confirmed=true` 兩種參數各自輪詢，直到查到剛才那筆交易的事件為止。

## 通過條件檢查

- [x] **API 呼叫成功，回傳資料中包含剛才發送的那筆轉帳事件**
  `only_confirmed=false` 輪詢第 1 次（廣播後約 23.4 秒）就查到，事件帶有 `"_unconfirmed": true` 標記；`only_confirmed=true` 則在廣播後約 63.1 秒才查到（等區塊確認後才出現，中間比 `only_confirmed=false` 晚了約 39.7 秒）。
- [x] **事件內容（from、to、金額、合約地址、交易雜湊）與實際發送的交易一致**

  | 欄位 | 實際發送 | API 回傳事件 | 一致 |
  |---|---|---|---|
  | from | `TYja3tVY9UkTw2HjH4LV6tUZaJV2GUakLm`（hex `41f9b6a3e4667ef01e607e57adca703f7210c736be`） | `0xf9b6a3e4667ef01e607e57adca703f7210c736be` | ✅ |
  | to | `TX36g4RCZKJmXio5qpcQAj8Qxpuo9MVX8Z`（hex `41e71705f7e9afcf3c046b2ab541f18d221e8f0336`） | `0xe71705f7e9afcf3c046b2ab541f18d221e8f0336` | ✅ |
  | 金額（raw, 6 decimals） | `12345678` | `12345678` | ✅ |
  | 合約位址 | `TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs` | `TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs` | ✅ |
  | 交易雜湊 | `223b7a074b2b7085e5aac22a4db54bc322299e2b8e2cbaaa5dd427deaaef05f4` | `223b7a074b2b7085e5aac22a4db54bc322299e2b8e2cbaaa5dd427deaaef05f4` | ✅ |

- [x] **記錄到「交易上鏈」到「事件可被查到」的實際延遲時間**
  - 廣播完成 → `only_confirmed=false` 可查到：約 **23.4 秒**
  - 廣播完成 → `only_confirmed=true` 可查到：約 **63.1 秒**
- [x] **確認 `only_confirmed` 等參數的行為符合預期**
  `only_confirmed=true` 時，交易雖已上鏈但尚未達確認數（區塊未確認）之前，確實不會出現在結果中；未確認狀態的事件則帶有明確的 `_unconfirmed: true` 標記可供區分，行為符合文件查證階段的預期。

## 過程中的觀察 / 沒有預期到的狀況

- **官方水龍頭的地區限制**：Nile（nileex.io）與 Shasta（shasta.tronex.io）官方水龍頭在本次驗證環境下都曾回報「Service is not available in your region」，跟驗證碼、操作方式無關，換水龍頭網址也無法繞過（官方文件列出的就只有這兩個網址）。最終是使用者自己的網路環境成功領取。**這是一個之前查證階段沒發現的實務風險**：如果正式開發/CI 環境的網路出口也落在被限制的地區，開發過程中要測試 TronGrid 整合會反覆卡在領不到測試幣這一步，需要另外設法（例如 Telegram/Discord 上的 TronFAQBot `!shasta_usdt ADDR` 指令，本次未實測，也可能受限）。
- **`only_confirmed=false` 與 `=true` 的可見延遲差距頗大**（約 40 秒），如果系統設計成只信任 `only_confirmed=true` 的結果（避免處理到之後被 reorg 的未確認交易），輪詢頻率與「多久才判定一筆入帳」的等待邏輯要把這個差距算進去，不能只用 `only_confirmed=false` 的延遲（約 23 秒）當作系統反應時間的預期基準。
- 其餘沒有出現版本落差或行為跟預期不符的狀況——API 回傳格式、欄位命名都跟技術選型階段查證的文件一致。

## 最終判定：**PASS**

四項通過條件全部符合。TronGrid 事件掃描 API 在 Shasta 測試網環境下確認可靠：呼叫得到、資料正確、延遲可量測、`only_confirmed` 行為符合預期，可以作為階段 03 技術架構設計的依據。

## 對階段 03 的提醒

- 正式系統的輪詢頻率設計，建議以 `only_confirmed=true` 的延遲（本次約 60 秒量級）為基準，而不是 `only_confirmed=false`；若要更快反應，可以先用 `only_confirmed=false` 做「預先提示」、再用 `only_confirmed=true` 做「正式入帳判定」的兩階段設計，但這需要額外處理未確認交易被 reorg 的情況（本次驗證未涵蓋）。
- 開發/CI 環境若要跑到會用到官方水龍頭的整合測試，要先確認網路出口地區不會被水龍頭擋掉，或準備好替代管道（Telegram/Discord bot、或請團隊成員手動先預先儲備一批測試幣）。
- 本次只驗證了單筆、單方向的轉帳事件，尚未涵蓋：高頻率並發轉帳時事件是否會漏抓、跨越多個區塊分頁查詢（`fingerprint`/分頁參數）的行為、以及事件掃描要如何從「最後處理到的區塊高度」斷點續掃——這些需要在架構設計階段另行處理。
