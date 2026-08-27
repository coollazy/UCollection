# 評估：Go 打包成 Docker Image 的可行性（實測）

- 背景：討論到正式服務改用 Go 開發、打包成 Docker image。比照 [Swift 打包 Docker 可行性評估](評估-Swift打包Docker可行性.md) 的做法，拿[驗證項目 1（HD 地址衍生）](技術可行性驗證.md)的邏輯用 Go 重做一次，在 Docker(Linux) 容器裡實測「能不能編、能不能跑、結果對不對」——同時這也是驗證項目 1 的第三條獨立實作路線（Node.js 兩條 + Swift 一條之後的第四條），再一次交叉核對同一組結果。
- 驗證日期：2026-08-27
- 程式碼位置（暫存目錄，未進正式專案結構）：
  - `/private/tmp/claude-501/-Users-user-Documents-Codes-UCollection/1aead0a5-6751-4d69-a793-72d85a6f502a/scratchpad/go-hd-verify/`（`go.mod`、`go.sum`、`main.go`、`Dockerfile`）

## 方法

用跟 [驗證結論-01-HD地址衍生.md](驗證結論-01-HD地址衍生.md)、[Swift 評估](評估-Swift打包Docker可行性.md) **完全相同的測試 xpub**，改用 Go 在官方 `golang:1.25`（Linux / aarch64 容器）裡重新算一次 index 0～4 的 Tron 地址，跟先前 Node.js／Swift 的結果逐筆比對。

用的函式庫：
- `github.com/btcsuite/btcd/btcutil` v1.2.0（`hdkeychain` 子套件）—— 解析 xpub、非硬化路徑的公鑰子鑰衍生（`ExtendedKey.Derive`），底層橢圓曲線運算來自 `github.com/decred/dcrd/dcrec/secp256k1/v4`
- `golang.org/x/crypto/sha3` v0.55.0 —— Keccak256（Tron 位址雜湊用）
- Base58Check 編碼 —— 直接用 `btcutil` 內建的 `base58.CheckEncode`（未手刻）

依賴的橢圓曲線函式庫（`decred/dcrd secp256k1`）跟 Node.js 兩條路線（`tiny-secp256k1`、npm `secp256k1`）、Swift 那條（`GigaBitcoin/secp256k1.swift`，包 libsecp256k1）都不同，是第四套獨立實作。

## 結果

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

## 過程中發現的風險

1. **`golang.org/x/crypto` 新版需要 Go 1.25 以上工具鏈。**
   一開始用 `golang:1.23` 容器建置，`go mod tidy` 直接失敗：`golang.org/x/crypto@v0.55.0 requires go >= 1.25.0`。換成 `golang:1.25` image 才成功。跟 Swift 那次「`secp256k1.swift` 需要 Swift 6.1 以上」是同一類風險——第三方套件的最新版本常常要求較新的工具鏈，正式環境的 CI/建置環境版本要跟著留意，不能固定用專案起始時的版本。
2. **只驗證了 arm64（Apple Silicon 上 Docker Desktop 的預設架構），沒驗證 amd64。**
   跟 Swift 評估的結論一樣：如果正式環境是 x86_64 雲主機，這次結果不能直接當作 amd64 也沒問題的證據，需要另外用 `docker buildx build --platform linux/amd64` 建一次並實測。不過因為整個執行檔是純 Go、無 cgo，理論上跨架構出問題的機率比 Swift 那條（混了 C 原始碼的 libsecp256k1）低，但沒有實測過就不能當結論。
3. **沒有遇到 Swift 那次的「套件宣稱跨平台、實測才發現不行」問題**（例如 `Base58Swift` 依賴 `CommonCrypto`）。這次用到的函式庫都是 Go 生態裡的主流、成熟套件（`btcsuite/btcd` 系列被廣泛用在其他鏈的錢包/節點軟體上），Linux 相容性風險本身就比較低，這是 Go 生態成熟度的差異，不是這次驗證方法比較寬鬆。

## 對「想用 Go 包成 Docker image」這個方向的結論

- **技術上可行，而且目前實測結果比 Swift 那條路線更順、image 更小、依賴更乾淨**：驗證項目 1 的核心邏輯完整用 Go 實作、在 Linux Docker 容器裡編譯執行，5 組地址與既有結果（Node.js、Swift）完全一致，過程沒有遇到任何相容性問題，最終 image 僅 11.3MB 且完全靜態連結。
- **尚未驗證、正式選型前建議補做**：
  - amd64 架構的建置與執行測試（如果正式環境不是 arm64）。
  - 驗證項目 2（TronGrid 事件掃描）如果之後也要用 Go 實作，用到的 HTTP client、TronGrid API 呼叫等額外依賴，仍要照這次的做法在 Linux 容器裡實測一次，不能只憑這次驗證項目 1 的結果就假設 Go 生態全部沒問題。
  - 若正式決定採用 Go，建議把 `go.mod` 的最低 Go 版本明確釘住（例如 1.25），並在 CI 環境同步更新，避免重蹈 Swift/Go 都出現過的「新版第三方套件要求更新工具鏈」問題。
