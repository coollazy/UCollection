# 評估：Swift 打包成 Docker Image 的可行性（實測）

- 背景：討論到正式服務是否用 Swift 開發、打包成 Docker image。與其只憑印象判斷，直接拿[驗證項目 1（HD 地址衍生）](技術可行性驗證.md)的邏輯用 Swift 重做一次，在 Docker(Linux) 容器裡實測「能不能編、能不能跑、結果對不對」。
- 驗證日期：2026-08-27
- 程式碼位置（暫存目錄，未進正式專案結構）：
  - `/private/tmp/claude-501/.../scratchpad/swift-hd-verify/`（`Package.swift`、`Sources/HDVerify/main.swift`、`Dockerfile`）

## 方法

用跟 [驗證結論-01-HD地址衍生.md](驗證結論-01-HD地址衍生.md) **完全相同的測試 xpub**，改用 Swift 在官方 `swift:6.1` Docker image（Linux / aarch64 容器）裡重新算一次 index 0～4 的 Tron 地址，跟 Node.js 那次的結果逐筆比對——等於是第三種完全獨立的實作（語言、函式庫、作業系統都不同）再交叉驗證一次。

用的函式庫：
- `GigaBitcoin/secp256k1.swift`（現為 21-DOT-DEV/swift-secp256k1）0.23.2 —— 包 libsecp256k1，`PublicKey.add(tweak:)` 對應 `secp256k1_ec_pubkey_tweak_add`，正是 BIP32 非硬化公鑰衍生需要的操作
- `CryptoSwift` 1.10.0 —— HMAC-SHA512（衍生用）、SHA256（Base58Check 校驗碼用）、Keccak256（Tron 位址雜湊用）
- Base58 / Base58Check —— **手刻**（原因見下方風險發現）

## 結果

- Swift 原始碼在 `swift:6.1`（Linux/aarch64）容器內用 `swift build -c release --static-swift-stdlib` 編譯成功。
- 用 multi-stage Dockerfile（builder: `swift:6.1`，runtime: `swift:6.1-slim`）打包成獨立 image，`docker run` 直接可執行，不需要額外裝東西。
- 5 組地址（index 0～4）跟 Node.js 版本 **完全一致**：

| index | Swift(Linux) 地址 | Node.js 地址 | 一致 |
|---|---|---|---|
| 0 | `TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH` | `TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH` | ✅ |
| 1 | `TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK` | `TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK` | ✅ |
| 2 | `TYJPRrdB5APNeRs4R7fYZSwW3TcrTKw2gx` | `TYJPRrdB5APNeRs4R7fYZSwW3TcrTKw2gx` | ✅ |
| 3 | `TRhVWK5XEDkQBDevcdCWW7RW51aRncty4W` | `TRhVWK5XEDkQBDevcdCWW7RW51aRncty4W` | ✅ |
| 4 | `TT2X2yyubp7qpAWYYNE5JQWBtoZ7ikQFsY` | `TT2X2yyubp7qpAWYYNE5JQWBtoZ7ikQFsY` | ✅ |

- 最終 image 大小：**378MB**（`swift:6.1-slim` 為基底）。
- 執行檔對系統函式庫依賴極少（`ldd` 結果：`libc`、`libm`、`libstdc++`、`libgcc_s`），Swift runtime 本身已透過 `--static-swift-stdlib` 靜態連結進執行檔內。

## 過程中發現的關鍵風險（這次得到具體證實，不是臆測）

1. **`Base58Swift`（keefertaylor/Base58Swift）在 Linux 上完全無法編譯。**
   套件自我描述是「pure swift implementation」，但原始碼裡 `import CommonCrypto`（Apple-only framework，用來算 SHA256 校驗碼），在 Linux 容器裡建置直接報錯：
   ```
   error: no such module 'CommonCrypto'
   ```
   這正是先前分析提醒過的「Swift 生態很多 crypto/wallet 套件是 iOS 導向，Linux 相容性未知」——這次證實風險是真的會發生，而且連套件自己的文件敘述都沒有提到這個限制，必須實際在目標環境（Linux 容器）建置一次才會發現，光看 README 或 SPM 描述看不出來。
   因應方式：拿掉這個依賴，改手刻約 40 行純 Swift 的 Base58/Base58Check 編碼（無任何 C library 依賴），驗證用的程式碼已包含在 `main.swift` 裡。

2. **`secp256k1.swift` 需要 Swift 6.1 以上工具鏈。**
   套件的 `Package.swift` 宣告 `swift-tools-version: 6.1`，且用了較新的 SPM traits 功能；舊版 Swift 工具鏈會直接 resolve 失敗。套件文件本身也明講「這些 API 尚不穩定，可能隨時變動」，正式採用前要留意升版風險，且要確保 CI/建置環境的 Swift 版本夠新。

3. **只驗證了 arm64（Apple Silicon 上 Docker Desktop 的預設架構），沒驗證 amd64。**
   如果正式環境是 x86_64 雲主機，這次結果不能直接當作 amd64 也沒問題的證據，需要另外用 `docker buildx build --platform linux/amd64` 建一次並實測。`secp256k1.swift` 裡混了 C 原始碼（libsecp256k1），跟純 Swift 套件比起來，跨架構出問題的機率理論上更高，這點目前還沒驗證過。

4. **`CryptoSwift` 在 Linux 上一切正常**，沒有發現平台相容性問題（純 Swift 實作）。

## 對「想用 Swift 包成 Docker image」這個方向的結論

- **技術上可行**：這次把驗證項目 1 的核心邏輯完整用 Swift 實作、在 Linux Docker 容器裡編譯執行，並與 Node.js 版本交叉驗證一致，證明 Swift + Docker 這條路走得通，不是空想。
- **但不能無腦選型**：Swift 的伺服器/Linux 生態遠比 Node.js 小，「看起來是純 Swift」的套件不一定真的跨平台，**必須每個要用的第三方套件都在 Linux 容器裡實際編過一次才能信任**，不能只看套件描述或在 macOS 本機測過就當作可用（這正是這次踩到 `Base58Swift` 的原因）。
- **尚未驗證、正式選型前建議補做**：
  - amd64 架構的建置與執行測試（如果正式環境不是 arm64）。
  - image 體積優化（378MB 偏大，`ldd` 顯示執行檔其實只依賴幾個標準系統函式庫，理論上有機會換更精簡的 base image，這次沒有進一步嘗試）。
  - 如果後續還要用其他 Swift 套件（例如 HTTP client 打 TronGrid API、資料庫驅動等），比照這次的做法，每一個都要在 Linux 容器裡實測一次，不要假設「Swift 套件 = 跨平台沒問題」。
