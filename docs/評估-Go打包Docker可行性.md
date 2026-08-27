# 評估：Go 打包成 Docker Image 的可行性（實測）

- 背景：討論到正式服務改用 Go 開發、打包成 Docker image。目的是在真正開工前先確認 Go 生態能撐起[技術可行性驗證.md](技術可行性驗證.md) 定義的三項核心技術假設，避免開工後才發現 Go 不支援某個關鍵操作。做法是把三項驗證的邏輯都用 Go 在 Docker(Linux) 容器裡重做一次。
- 驗證日期：2026-08-27
- 目前涵蓋範圍：驗證項目 1（HD 地址衍生）、驗證項目 2（TronGrid 事件掃描）、驗證項目 3（助記詞硬化衍生）— 三項全部用 Go 重驗過。

---

## 驗證項目 1：HD 地址衍生正確性

- 比照 [Swift 打包 Docker 可行性評估](評估-Swift打包Docker可行性.md) 的做法，拿[驗證項目 1（HD 地址衍生）](技術可行性驗證.md)的邏輯用 Go 重做一次，在 Docker(Linux) 容器裡實測「能不能編、能不能跑、結果對不對」——同時這也是驗證項目 1 的第三條獨立實作路線（Node.js 兩條 + Swift 一條之後的第四條），再一次交叉核對同一組結果。
- 程式碼位置（暫存目錄，未進正式專案結構）：
  - `/private/tmp/claude-501/-Users-user-Documents-Codes-UCollection/1aead0a5-6751-4d69-a793-72d85a6f502a/scratchpad/go-hd-verify/`（`go.mod`、`go.sum`、`main.go`、`Dockerfile`）

### 方法

用跟 [驗證結論-01-HD地址衍生.md](驗證結論-01-HD地址衍生.md)、[Swift 評估](評估-Swift打包Docker可行性.md) **完全相同的測試 xpub**，改用 Go 在官方 `golang:1.25`（Linux / aarch64 容器）裡重新算一次 index 0～4 的 Tron 地址，跟先前 Node.js／Swift 的結果逐筆比對。

用的函式庫：
- `github.com/btcsuite/btcd/btcutil` v1.2.0（`hdkeychain` 子套件）—— 解析 xpub、非硬化路徑的公鑰子鑰衍生（`ExtendedKey.Derive`），底層橢圓曲線運算來自 `github.com/decred/dcrd/dcrec/secp256k1/v4`
- `golang.org/x/crypto/sha3` v0.55.0 —— Keccak256（Tron 位址雜湊用）
- Base58Check 編碼 —— 直接用 `btcutil` 內建的 `base58.CheckEncode`（未手刻）

依賴的橢圓曲線函式庫（`decred/dcrd secp256k1`）跟 Node.js 兩條路線（`tiny-secp256k1`、npm `secp256k1`）、Swift 那條（`GigaBitcoin/secp256k1.swift`，包 libsecp256k1）都不同，是第四套獨立實作。

### 結果

- Go 原始碼在 `golang:1.25`（Linux/aarch64）容器內 `go build`（`CGO_ENABLED=0`）編譯成功，**過程沒有踩到任何相容性問題**——`go mod tidy` 一次解析成功，程式邏輯第一次執行就與既有結果完全一致，不像 Swift 那次踩了兩個套件相容性坑。
- 用 multi-stage Dockerfile（builder: `golang:1.25`，runtime: `gcr.io/distroless/static-debian12`）打包成獨立 image，`docker run` 直接可執行。
- 5 組地址（index 0～4）跟 Node.js／Swift 版本 **完全一致**：

| index | Go(Linux) 地址 | Node.js / Swift 地址 | 一致 |
|---|---|---|---|
| 0 | `TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH` | `TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH` | ✅ |
| 1 | `TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK` | `TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK` | ✅ |
| 2 | `TYJPRrdB5APNeRs4R7fYZSwW3TcrTKw2gx` | `TYJPRrdB5APNeRs4R7fYZSwW3TcrTKw2gx` | ✅ |
| 3 | `TRhVWK5XEDkQBDevcdCWW7RW51aRncty4W` | `TRhVWK5XEDkQBDevcdCWW7RW51aRncty4W` | ✅ |
| 4 | `TT2X2yyubp7qpAWYYNE5JQWBtoZ7ikQFsY` | `TT2X2yyubp7qpAWYYNE5JQWBtoZ7ikQFsY` | ✅ |

- 最終 image 大小：**11.3MB**（`gcr.io/distroless/static-debian12` 為基底），遠小於 Swift 那次的 378MB。
- 執行檔對系統函式庫**完全零依賴**：`file` 顯示 `statically linked`，在容器內對它跑 `ldd` 回報「not a dynamic executable」。這比 Swift 版本（仍依賴 `libc`/`libm`/`libstdc++`/`libgcc_s`）更乾淨，原因是全部用純 Go 實作（含橢圓曲線運算），沒有透過 cgo 包 C 函式庫（Swift 那條路線的 `secp256k1.swift` 是包 libsecp256k1 這個 C library）。

### 過程中發現的風險

1. **`golang.org/x/crypto` 新版需要 Go 1.25 以上工具鏈。**
   一開始用 `golang:1.23` 容器建置，`go mod tidy` 直接失敗：`golang.org/x/crypto@v0.55.0 requires go >= 1.25.0`。換成 `golang:1.25` image 才成功。跟 Swift 那次「`secp256k1.swift` 需要 Swift 6.1 以上」是同一類風險——第三方套件的最新版本常常要求較新的工具鏈，正式環境的 CI/建置環境版本要跟著留意，不能固定用專案起始時的版本。
2. **只驗證了 arm64（Apple Silicon 上 Docker Desktop 的預設架構），沒驗證 amd64。**
   跟 Swift 評估的結論一樣：如果正式環境是 x86_64 雲主機，這次結果不能直接當作 amd64 也沒問題的證據，需要另外用 `docker buildx build --platform linux/amd64` 建一次並實測。不過因為整個執行檔是純 Go、無 cgo，理論上跨架構出問題的機率比 Swift 那條（混了 C 原始碼的 libsecp256k1）低，但沒有實測過就不能當結論。
3. **沒有遇到 Swift 那次的「套件宣稱跨平台、實測才發現不行」問題**（例如 `Base58Swift` 依賴 `CommonCrypto`）。這次用到的函式庫都是 Go 生態裡的主流、成熟套件（`btcsuite/btcd` 系列被廣泛用在其他鏈的錢包/節點軟體上），Linux 相容性風險本身就比較低，這是 Go 生態成熟度的差異，不是這次驗證方法比較寬鬆。

### 小結

技術上可行，而且比 Swift 那條路線更順、image 更小、依賴更乾淨：5 組地址與既有結果（Node.js、Swift）完全一致，過程沒有遇到任何相容性問題，最終 image 僅 11.3MB 且完全靜態連結。

---

## 驗證項目 2：TronGrid 測試網事件掃描

- 目的：確認 Go 不只能「算地址」，還能完成正式系統實際會做的事——**簽署交易、廣播上鏈、呼叫 TronGrid 事件 API**。沿用[驗證結論-02](驗證結論-02-TronGrid事件掃描.md)已經領好測試幣的 Shasta 測試網錢包（`TYja3tVY9UkTw2HjH4LV6tUZaJV2GUakLm`），改用 Go 送一筆 USDT 測試代幣轉帳並查詢事件。
- 程式碼位置：`/private/tmp/claude-501/-Users-user-Documents-Codes-UCollection/1aead0a5-6751-4d69-a793-72d85a6f502a/scratchpad/go-item2-trongrid/`（`go.mod`、`go.sum`、`main.go`、`Dockerfile`）

### 方法與函式庫

- `github.com/fbsobreira/gotron-sdk` v0.26.0 —— Go 生態裡功能最完整的 Tron SDK（gRPC client、HD 錢包、TRC20/TRC10、多簽、質押等），用其 `client.TRC20Send()` 建構未簽名的 TRC20 轉帳交易、`client.Broadcast()` 廣播已簽名交易。gRPC 端點：`grpc.shasta.trongrid.io:50051`。
- `github.com/ethereum/go-ethereum` v1.17.5 的 `crypto` 套件 —— 用於簽署交易（`crypto.Sign`，產出 Tron 要求的 65 bytes r‖s‖v 格式簽名，這格式跟 Ethereum 相同）。
- 事件查詢沿用純標準庫 `net/http` 直接呼叫 TronGrid REST API（`GET /v1/contracts/{contract}/events`），不需要額外套件。

### 結果

- **第一次嘗試廣播失敗**：`Validate signature error: ... is signed by TYLgAvqJmHFgxPgdfdCU1mZwtxMXFFTfqe but it is not contained of permission`——簽名技術上有效，但復原出來的位址不是預期的付款位址。
- **根因**：一開始用 `Keccak256(marshal(raw_data))` 當作簽名用的雜湊，這是**錯的**。Tron 的位址衍生用 Keccak256（Ethereum 風格），但**交易簽名用的雜湊是 SHA256**，兩者是分開的機制，混用會讓簽名數學上「有效」（能復原出一個地址），但復原出來的地址是錯的（因為簽的是錯誤訊息的雜湊）。改成直接使用 gRPC 回應裡 `TransactionExtention.GetTxid()`（節點端已經算好的 raw_data 雜湊，其實就是 SHA256）簽名後，問題解決、廣播成功。
- 修正後，兩次測試（一次直接跑執行檔、一次跑打包好的 Docker image）都成功送出交易並在 TronGrid 查到事件：

  | 執行方式 | txID | `only_confirmed=false` 延遲 | `only_confirmed=true` 延遲 |
  |---|---|---|---|
  | 直接執行 | `f566105768ba...` | 5.1 秒 | 60.7 秒 |
  | Docker image | `5670c4b68d7a...` | 4.9 秒 | 62.8 秒 |

  延遲數量級與 Node.js 版（23 秒／63 秒）一致（`only_confirmed=false` 這次更快，屬於區塊時序的正常波動，不是實作差異）。
- multi-stage Docker image（builder: `golang:1.25`，runtime: `gcr.io/distroless/static-debian12`）大小：**26.1MB**——比項目 1 的 11.3MB 大，主因是引入了 `go-ethereum`（簽名）與 gRPC 相關依賴，但仍屬輕量。

### 過程中發現的風險

1. **簽名雜湊函式選錯（Keccak256 vs SHA256）是這次最大的發現**：這正是技術可行性驗證階段存在的意義——這類「簽名技術上成功、但復原出錯誤地址」的錯誤在單元測試或本地模擬環境可能不會被抓到（如果測試沒有真的送去節點驗證簽名），但一旦在正式環境發生，後果是簽出的交易永遠無法廣播成功（節點會拒絕），或更糟——如果實作方式不同，有可能簽出「看似有效但簽錯內容」的交易。此風險在 Node.js／tronweb 或 gotron-sdk 高階 API（如範例程式碼用的 `transaction.Controller`）通常會被函式庫封裝掉，但這次為了理解機制刻意手刻簽名步驟，才把這個坑暴露出來。
2. **gotron-sdk 的依賴樹相當龐大**：`go mod tidy` 一次拉入 go-ethereum、grpc、OpenTelemetry 等一大批間接依賴（`go.sum` 有數十個套件），這是選用「功能完整的第三方 SDK」必然的代價，非相容性問題，但會拉長建置時間、增加 image 大小（26.1MB vs 純手刻的 11.3MB），選型時要納入考量。
3. 只驗證了 arm64，amd64 未測試（與項目 1、Swift 評估的既有風險項一致）。

### 小結

**技術上可行**：Go 能完整走完「建構 TRC20 交易 → 簽名 → 廣播上鏈 → TronGrid 事件查詢確認」全流程，且延遲數量級與 Node.js 版一致。但過程中發現了一個**必須注意的實作細節**（簽名雜湊函式選錯），代表若正式採用 Go 且選擇繞過 SDK 高階封裝、自行處理簽名，需要對 Tron 簽名機制有正確理解，不能憑直覺或憑其他鏈（如 Ethereum）的經驗類推。

---

## 驗證項目 3：助記詞/私鑰硬化衍生正確性

- 目的：確認 Go 能正確處理 5.9 手動歸集功能需要的「助記詞 → 硬化路徑衍生 → 私鑰 → 簽名」流程。
- 程式碼位置：`/private/tmp/claude-501/-Users-user-Documents-Codes-UCollection/1aead0a5-6751-4d69-a793-72d85a6f502a/scratchpad/go-item3-mnemonic/`（`go.mod`、`go.sum`、`main.go`、`Dockerfile`）

### 方法與函式庫

- `github.com/tyler-smith/go-bip39` v1.1.0 —— 助記詞轉 seed。
- `github.com/btcsuite/btcd/btcutil/hdkeychain` v1.2.0 —— 完整硬化＋非硬化路徑衍生（`ExtendedKey.Derive()`，硬化索引用 `hdkeychain.HardenedKeyStart + n`），跟項目 1 用的是同一套函式庫，但這次是私鑰衍生而非公鑰衍生。
- `github.com/btcsuite/btcd/btcec/v2/ecdsa` —— 簽名與驗章（`ecdsa.Sign` / `sig.Verify`）。

### 結果

用跟[驗證結論-03](驗證結論-03-助記詞私鑰硬化衍生.md)完全相同的測試助記詞，衍生 index 0～4：

- 5 組私鑰、地址跟 Node.js 版（驗證結論-01、驗證結論-03）**逐一比對完全一致**（含私鑰本身，不只是地址字串）。
- 5 組簽名/驗章 roundtrip 全部通過。
- **第一次執行就全部通過，沒有踩到任何相容性或邏輯問題**——這點跟項目 1 一樣順利，且沒有重演項目 2 的簽名雜湊誤用問題（這次直接對本地產生的訊息雜湊簽名/驗章，不涉及 Tron 協定層的雜湊選擇）。
- multi-stage Docker image 大小：**11.5MB**，跟項目 1 相近（同樣是純 Go、無 cgo、靜態連結）。

### 小結

**技術上可行**：Go 可以正確重現 Node.js 版驗證項目 3 的完整結果（含與驗證項目 1 的橋接一致性），可以作為 5.9 手動歸集功能若採用 Go 開發（不論前端仍用 JS/TronWeb、僅後端輔助邏輯用 Go，或整個流程都用 Go 驗證/測試）的依據。

---

## 整體結論：Go 是否能撐起這套系統的核心技術假設

三項技術可行性驗證（HD 地址衍生、TronGrid 事件掃描、助記詞硬化衍生）都已用 Go 在 Docker(Linux) 容器裡重做過，**全部技術上可行**，跟 Node.js 版本的結果完全一致。

- **項目 1、3**：第一次執行就通過，無相容性問題，image 都在 11MB 級距、完全靜態連結、零系統依賴。
- **項目 2**：功能上可行，但過程中發現一個**真實的坑**——簽名雜湊函式選錯（Keccak256 vs SHA256），修正後才成功。這代表若正式決定用 Go 開發，**不能假設所有環節都會像項目 1、3 那樣一次就對**，尤其是繞過高階 SDK 封裝、自行處理簽名/協定細節的部分，需要團隊對 Tron 協定機制有正確理解或有充分的整合測試覆蓋。

**尚未驗證、正式選型前建議補做**：
- amd64 架構的建置與執行測試（三項驗證都只測過 arm64）。
- gotron-sdk 是否有其他協定細節（例如多簽、質押、TRC10 等，若未來用得到）存在類似項目 2 那種「文件沒講清楚、要實測才發現」的坑，建議每個要用到的功能都個別實測，不要因為項目 1/3 很順利就假設項目 2 的坑是特例。
- 若正式決定採用 Go，建議把 `go.mod` 的最低 Go 版本明確釘住（例如 1.25），並在 CI 環境同步更新。
