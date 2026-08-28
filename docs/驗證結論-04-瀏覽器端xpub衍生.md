# 驗證結論：驗證項目 4（瀏覽器端助記詞 → xpub 衍生正確性）

- 對應：[技術可行性驗證](技術可行性驗證.md) 驗證項目 4
- 驗證日期：2026-08-28
- 驗證用程式碼位置：`/private/tmp/claude-501/-Users-user-Documents-Codes-UCollection/796e4128-2eb2-4af5-b87b-2e96dc604ffa/scratchpad/browser-verify/`（暫存目錄，未進正式專案結構，可丟棄）
- 測試瀏覽器：Chrome 151.0.0.0（macOS，透過 `claude-in-chrome` 遠端操控真實瀏覽器分頁執行，非 Node.js/jsdom 模擬環境）

## 使用的函式庫與版本

兩條路線刻意選用**底層橢圓曲線函式庫完全不同**的組合，兩者皆為純 JavaScript（無 WASM、無原生模組），可直接被 esbuild 打包進單一靜態檔案：

| | 路線 A | 路線 B |
|---|---|---|
| BIP39（助記詞→seed / 產生助記詞） | `bip39` 3.1.0（與驗證結論-01/03 相同套件） | `@scure/bip39` 2.3.0（完全獨立實作） |
| HD 衍生庫（含硬化路徑） | `hdkey` 2.1.0（與驗證結論-03 路線 B 相同套件） | `@scure/bip32` 2.3.0（完全獨立實作） |
| 橢圓曲線底層 | `secp256k1` 5.0.2（瀏覽器環境走其 `browser` 欄位指向的純 JS `elliptic` 後備實作，非原生 binding） | `@noble/curves` 2.4.0（純 JS，無 WASM） |
| Tron 位址編碼 | 共用：`@noble/hashes` 2.4.0（keccak256）+ `@noble/curves`（公鑰解壓縮）+ `bs58check` 4.0.0，手寫拼接 `0x41` 前綴 | 同左 |

位址編碼刻意兩條路線共用同一份實作，因為本項目要驗證的關鍵假設是「瀏覽器能否正確做硬化路徑衍生」，位址編碼正確性已在驗證項目 1、3（Node.js/Go 環境）用多套獨立實作交叉驗證過，不是本次重點。

打包工具：`esbuild` 0.28.2，`--bundle --platform=browser`，另外用 `buffer`/`process`/`crypto-browserify`/`stream-browserify`/`events` 幾個瀏覽器 polyfill 套件補齊 `hdkey`/`bip39` 依賴的 Node.js 核心模組（`Buffer`、`crypto.createHmac` 等），全部打包進兩個靜態 `.js` 檔案（`polyfill.bundle.js`、`verify.bundle.js`），過程不連外網、不經 CDN。

## 測試助記詞來源

**沿用驗證項目 1、3 完全相同**的公開 BIP39 標準測試助記詞與帳戶路徑：
- 助記詞：`abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about`
- 帳戶路徑：`m/44'/195'/0'`（硬化）
- 完整衍生路徑：`m/44'/195'/0'/0/{index}`

## 驗證結果

### 1. 已知測試助記詞 → xpub → 地址

| 檢查項 | 結果 |
|---|---|
| 瀏覽器路線 A 算出的 xpub | `xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaQgmv8D45oNUKx6DZMNZBCd` |
| 瀏覽器路線 B 算出的 xpub | 與路線 A 完全相同 |
| 與驗證結論-01/03 記錄的 xpub 比對 | ✅ 完全一致 |
| index 0～4 衍生地址（路線 A、B） | `TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH`、`TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK`、`TYJPRrdB5APNeRs4R7fYZSwW3TcrTKw2gx`、`TRhVWK5XEDkQBDevcdCWW7RW51aRncty4W`、`TT2X2yyubp7qpAWYYNE5JQWBtoZ7ikQFsY` |
| 與驗證結論-01 的 5 個地址逐一比對 | ✅ 完全一致（5/5） |

### 2. 瀏覽器隨機產生新助記詞（模擬 xpub 取得方式 3）

- 隨機產生助記詞：12 個單字，`bip39.validateMnemonic()`（路線 A）與 `@scure/bip39.validateMnemonic()`（路線 B）皆判定格式合法。
- 用這組新助記詞，路線 A、B 各自獨立算出的 xpub 完全一致（例：`xpub6C2bJR89G7Upb9mYyy1a6hCGDus6M31wKa4B1yYnMAdBTBSXeA52LGgjUTUKcXj56MBa1yC5eo5gPjEDkDSaUGAd42iUmfaYXh7Etoxmr8C`，另一次獨立執行為 `xpub6DB7DYi7dG7wrz2W7zep9r66RbkERwmkmVz7bWCoQ56DfGQvAsLehhrjrYkjddpcgdiiSFiaKuD7Bo7QHKjPsa97bzFa1tm18wYpfAw13F4`，重複跑了兩次，兩次的路線 A/B 各自都一致，只是每次隨機助記詞不同導致 xpub 不同，符合預期）。

## 通過條件檢查

- [x] 瀏覽器算出的 xpub 與驗證結論-03（Node.js/Go 版本）完全一致。
- [x] 用該 xpub 衍生的 5 個地址（index 0～4）與驗證結論-01 逐一一致。
- [x] 瀏覽器隨機產生的新助記詞格式合法，且用第二套獨立方法交叉核對算出的 xpub 一致。

## 過程中的觀察 / 沒有預期到的狀況

- 原先計畫路線 A 用 `bip32` + `tiny-secp256k1`（與驗證結論-01/03 路線 A 相同），但 `tiny-secp256k1` 的瀏覽器版透過 `import ... from "./secp256k1.wasm"` 載入 WASM，這種寫法依賴 webpack 等特定打包工具的資源載入慣例，`esbuild` 原生不支援、需要額外插件才能處理。為了不讓打包工具本身的相容性問題模糊掉本項目真正要驗證的重點（衍生數學正確性），改選 `hdkey` + `secp256k1`（其 `secp256k1` 套件已用 `package.json` 的 `browser` 欄位正確指向純 JS 的 `elliptic` 後備實作，`esbuild` 可直接識別），純 JS、無 WASM，仍是與驗證結論-03 路線 B 相同的既有函式庫組合。**提醒階段 03/04**：若之後正式開發選用會用到 WASM 的加密函式庫（`tiny-secp256k1` 是常見選項之一），需要額外設定 bundler 的 WASM loader，不能直接假設 `esbuild`/`bitcoinjs` 系列文件範例的打包指令可以照搬。
- `@noble/curves` 2.4.0 的 API 與較舊版本不同：解壓縮公鑰要用 `secp256k1.Point.fromHex()` + `.toBytes(false)`，而非部分教學文章仍在用的 `secp256k1.ProjectivePoint`（該類別在此版本已不存在）。若正式開發時參考網路上針對舊版 `@noble/curves` 寫的範例程式碼，會直接卡在這一步。
- `hdkey`/`bip39` 依賴 Node.js 核心模組（`Buffer`、`crypto`、`stream`、`events`），純瀏覽器環境需另外用 `buffer`/`crypto-browserify`/`stream-browserify`/`events` 等 polyfill 套件補齊，並用 `esbuild --alias` 手動接上，這是額外的打包設定成本，但不影響衍生結果正確性——這點驗證項目 5 有再更嚴格的環境（加上 CSP）下重新測過一次，結果一致。
- 其餘沒有出現版本落差或行為跟預期不符的狀況。

## 最終判定：**PASS**

三項通過條件全部符合。瀏覽器 JavaScript 環境能夠正確完成「助記詞 → 硬化路徑衍生 → 帳戶層級 xpub → 非硬化子公鑰 → Tron 地址」這條技術路徑，且用兩套完全獨立的函式庫組合（不同 BIP39/BIP32 實作、不同橢圓曲線底層）交叉驗證一致，並與伺服器端（Node.js/Go）驗證結果完全收斂。可以作為需求書 5.1「xpub 取得方式 2、3」（商戶輸入既有助記詞 / 系統於瀏覽器端產生新助記詞）在階段 03 架構設計中的技術依據。

## 對階段 03 的提醒

- 正式開發若選用會用到 WASM 的加密函式庫，需另外處理 bundler 的 WASM loader 設定（見上方觀察）；若想避開這個額外複雜度，`hdkey`/`@scure/bip32` 這類純 JS 實作是更省事的選擇。
- 本次驗證確認的是「瀏覽器端衍生數學正確」，尚未涵蓋：真實使用者在瀏覽器貼上/輸入助記詞的 UI 流程與其防護措施（例如避免助記詞被瀏覽器擴充功能、剪貼簿歷史等意外留存）、xpub 算出後如何安全傳回後端系統。這些屬於介面/流程設計範疇，需在階段 03 架構設計時另行處理。
- 離線打包與嚴格 CSP 相容性見驗證項目 5（[驗證結論-05](驗證結論-05-離線打包與嚴格CSP相容性.md)），與本項目使用同一份程式碼、同一組函式庫。
