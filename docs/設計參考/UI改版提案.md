# UCollection 後台 UI 改版提案（暗黑加密風格）

> 狀態：**設計已全數拍板，待排入實作**。本文只描述設計規範與導入計畫，尚未動任何 `internal/**/templates/*.html`、`web/static/css/`、`.go` 檔案。
> 決策依據：使用者於本次會話對 5 個取捨問題＋3 個次要項的回覆（見文末「決策紀錄」）。
> 可視預覽：同資料夾 `mockup-dashboard.html`、`mockup-orders-list.html`、`mockup-login.html`，雙擊即可在瀏覽器開啟（完全自包含、無任何 CDN/外部依賴）。

---

## 0. 設計目標與邊界

- **質感目標**：貼近參考圖 `後台暗黑風格參考.png` 的「深色沉穩、專業加密、資訊密度高、青綠強調色」，但**不逐像素複製其頁面內容/資訊架構**——UCollection 的實際頁面（統計卡、同步狀態、訂單表、歸集流程、稽核日誌）與參考圖不同，只借用其視覺語言。
- **技術鐵律不變**：Go `html/template` + htmx，零 Node.js 建置鏈。本改版**不引入任何前端框架/建置流程**，只新增一支 **靜態 CSS 檔** 與 **本地字型檔**。
- **零外部依賴**：所有視覺資源（字型、圖示）一律本地離線檔或系統字型，**不使用任何 CDN**（單租戶自架精神＋高風險頁嚴格 CSP 皆要求如此）。
- **不破壞既有機制**：`{{ts}}`（`<time>` + localtime.js，JS 未執行時 fallback 顯示 UTC 文字）、`{{amt}}`、`{{desc}}` 全部保留；**配色與版面不建立在「JS 一定要跑完才看得懂」的前提上**（CSS 不依賴 JS，`<time>` 的文字 fallback 在暗底仍清晰可讀）。

## 1. 本次拍板的方向（來自決策紀錄）

| 維度 | 決定 | 代價（使用者已接受） |
|---|---|---|
| 導覽結構 | **左側邊欄**（取代頂部橫向 nav + `<details>` 折疊選單） | 要動全部約 17 個模板的外層版面結構 |
| 改版力度 | **全面重做**：表格→卡片化/樣式化、狀態值→分色 pill、表單/按鈕元件全面重製 | 要逐一檢視每個模板的 HTML 語意 |
| 圖示與字體 | **自訂 `@font-face` 字型檔** + 本地 inline SVG 圖示 | 需在 2 個高風險頁 CSP 加 `font-src 'self'`（見第 7 節） |
| Dashboard 圖表 | **這次不做**，列為未來獨立項目 | 本次只處理配色/排版，不含資料視覺化 |
| 登入/TOTP 極簡頁 | **一併納入** | login/totp_verify/totp_setup/reverify 也套暗黑風（置中卡片式，無側邊欄） |

---

## 2. 色票（Color Tokens）

全部以 CSS 變數集中管理（`:root{}`），實作時放在 `web/static/css/admin.css` 最上方。

### 2.1 中性色（背景 / 前景 / 邊框）

| Token | Hex | 用途 |
|---|---|---|
| `--bg` | `#0B0F14` | App 最底層背景（近黑、帶微藍） |
| `--bg-elev` | `#0E141B` | 側邊欄、輸入框底色（比 app 略淺一階） |
| `--surface` | `#131A22` | 卡片 / 面板 / 表格容器 |
| `--surface-2` | `#1A222C` | 表頭、hover、次級按鈕底 |
| `--border` | `#232D38` | 一般分隔線 / 卡片邊框 |
| `--border-strong` | `#2E3A47` | 輸入框 / 按鈕邊框 |
| `--text` | `#E6EDF3` | 主要文字 |
| `--text-2` | `#9BA9B7` | 次要文字 / label |
| `--text-muted` | `#64748B` | 弱化文字 / 表頭 / placeholder |

### 2.2 強調色（品牌青綠）

| Token | Hex / 值 | 用途 |
|---|---|---|
| `--accent` | `#2DD4A7` | 品牌主色：主按鈕、active nav、連結、重點數字 |
| `--accent-hover` | `#4EE7BE` | hover 態 |
| `--accent-tint` | `rgba(45,212,167,.12)` | active nav 底色、focus ring、選中 chip 底 |
| `--on-accent` | `#04110D` | 疊在青綠實心按鈕上的深色文字（確保對比） |

### 2.3 狀態語意色（在暗底皆已調亮以確保對比）

每個訂單/系統狀態對應一種語意色，pill、flash、異常標記共用。

| Token | Hex | pill 底色（tint） | 對應狀態 / 語意 |
|---|---|---|---|
| `--success` | `#34D399` | `rgba(52,211,153,.14)` | COMPLETED、操作成功 flash |
| `--warning` | `#FBBF24` | `rgba(251,191,36,.14)` | CONFIRMING（等待確認中） |
| `--info` | `#60A5FA` | `rgba(96,165,250,.14)` | PENDING（等待付款）、藍色提示 flash |
| `--cyan` | `#22D3EE` | `rgba(34,211,238,.14)` | OVERPAID（溢付，需注意但非錯誤） |
| `--neutral` | `#94A3B8` | `rgba(148,163,184,.14)` | EXPIRED（終態、弱化） |
| `--danger` | `#F87171` | `rgba(248,113,113,.14)` | CONFIRMATION_STALLED、異常訂單、ILLEGAL_STATE_TRANSITION、失敗 flash |

> **狀態→顏色對照表（實作時寫死在模板 class）**
> `PENDING`→`pill--pending`(info)、`CONFIRMING`→`pill--confirming`(warning)、`COMPLETED`→`pill--completed`(success)、`OVERPAID`→`pill--overpaid`(cyan)、`EXPIRED`→`pill--expired`(neutral)、`CONFIRMATION_STALLED`→`pill--stalled`(danger)。歸集狀態 `consolidated`→success、`pending`→neutral。

---

## 3. 字體

### 3.1 策略：Latin 自訂字型 + CJK 系統字型（**不自架 CJK 字型檔**）

繁體中文網頁字型檔動輒數 MB（Noto Sans TC / 思源黑體全字集），**自架整份 CJK woff2 會嚴重拖慢載入、也不符單租戶自架的輕量精神**。因此策略是：

- **Latin / 數字 / 位址 / 交易雜湊**：自架 `@font-face` 字型（本次 Q3 選定要做的部分）。加密後台大量顯示金額、訂單 ID、TRON 位址、tx hash，這些最吃「等寬、tabular 數字、0/O・1/l 可辨識」——自訂字型的價值集中在這裡。
- **中文**：走系統字型堆疊（PingFang TC / Microsoft JhengHei / Noto Sans TC 擇一，使用者機器本來就有），**不發生任何網路請求**。

### 3.2 建議字型（皆為 SIL OFL 授權，可自由自架；**已拍板採用**）

| 用途 | 首選建議 | 次選 | 說明 |
|---|---|---|---|
| UI Sans（Latin） | **Inter**（variable woff2，可只子集化 Latin） | IBM Plex Sans | 介面/標題/一般數字 |
| Mono（位址/hash/金額） | **JetBrains Mono** | IBM Plex Mono | 等寬、tabular、字元辨識度高 |

字型堆疊（實作於 `--font-sans` / `--font-mono`）：

```
--font-sans: "Inter", "PingFang TC", "Microsoft JhengHei", "Noto Sans TC",
             system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
--font-mono: "JetBrains Mono", "IBM Plex Mono", ui-monospace,
             SFMono-Regular, Menlo, Consolas, monospace;
```

> 字型檔放置：`web/static/fonts/`（例如 `inter-latin.woff2`、`jetbrains-mono-latin.woff2`），用 `@font-face { src: url("/static/fonts/...") format("woff2"); font-display: swap; }` 宣告。務必做 **Latin 子集化**（unicode-range 限 Latin + 標點 + 數字符號）把每檔壓到數十 KB。
> **mockup 說明**：mockup 為維持「雙擊即開、零外部請求」，`font-family` 直接寫上述堆疊——若你的機器裝了 Inter/JetBrains Mono 就會顯示，否則自動 fallback 到系統字型，觀感一致。正式版才會真的載入自架 woff2。

---

## 4. 基礎 tokens（間距 / 圓角 / 陰影）

| 類別 | Token | 值 |
|---|---|---|
| 間距 | `--sp1..--sp10` | 4 / 8 / 12 / 16 / 20 / 24 / 32 / 40 px |
| 圓角 | `--r-sm` `--r-md` `--r-lg` `--r-pill` | 6 / 10 / 14 / 999 px |
| 陰影 | `--shadow` | `0 1px 2px rgba(0,0,0,.5), 0 12px 28px -16px rgba(0,0,0,.7)` |
| 側邊欄寬 | `--sidebar-w` | 240 px |

> 暗底上陰影可見度低，**層次主要靠背景階（bg → surface → surface-2）+ 邊框**，陰影只作卡片的輕微浮起。

---

## 5. 元件樣式規範

### 5.1 側邊欄 nav（取代頂部 nav + `<details>`）

- 固定左側、寬 240px、`--bg-elev` 底、右側 1px 邊框、`sticky` 滿版高。
- **頂部品牌區**：inline SVG 標誌（青綠六角幣 mark）+ 「UCollection」字樣。
- **主導覽項**（icon + 文字）：儀表板 / 訂單列表 / 手動歸集 / 通知歷史 / 稽核日誌。
- **設定分組**：以 `<details>` element 保留可折疊語意（無 JS 亦可運作，比照現有 `_nav.html` 做法），子項為代收主錢包 / API Key 管理 / Webhook 設定 / 參數設定 / 歸集地址簿 / 手續費來源地址簿 / 修改密碼。
- **active 態**：`--accent-tint` 底 + `--accent` 文字/圖示。
- **底部**：登出（`RequireSession` 的 `POST /admin/logout` 表單按鈕）固定貼底。
- **響應式**：視窗 < 900px 時側邊欄轉為頂部橫列（或 icon-only），主內容取消左邊距。

### 5.2 卡片 / 統計磚 / KV 明細表

- `.card`：`--surface` 底 + `--border` 邊 + `--r-lg` 圓角 + `--shadow`。卡片標題 `.card-title` 用 `--text-2` 小字。
- `.stat`（統計磚）：小 label（`--text-2`）+ 大數字（`--font-mono`, 28px, 可用 `--accent`）+ 單位。dashboard 三個統計（總收款/待處理/異常）用 `.grid-3` 排。
- `.kv`（order_detail 的「基本資料」等鍵值表）：左欄 label（`--text-2`, 固定寬）、右欄值（位址/hash 用 `.mono`）。

### 5.3 表格（表格密集頁全面樣式化，非砍掉重練成卡片）

訂單列表 / 稽核日誌 / 入帳明細 / 待歸集列表這類**高密度資料仍維持表格**（卡片化會犧牲比較性與密度），但：

- 外層 `.table-wrap`（`overflow-x:auto` + 邊框 + 圓角，寬表可橫向捲動、頁面本身不橫捲）。
- 移除 `<table border="1" cellpadding="4">` 的 `border`/`cellpadding` 屬性，改用 class。
- 表頭 `.table th`：`--surface-2` 底、小字大寫、`--text-muted`。
- 資料列 `hover` 變 `--surface-2`；分隔線用 `--border`。
- 位址/hash/ID 用 `.mono`（`--text-2`）；金額欄 `.num` 右對齊 + `font-variant-numeric: tabular-nums`。
- 狀態欄改放 `.pill`（見 5.5）。

### 5.4 表單 / 篩選 / 按鈕 / TOTP 輸入

- 輸入框：`--bg-elev` 底、`--border-strong` 邊、`--r-md`；focus 態 `--accent` 邊 + `--accent-tint` 3px ring。
- `.field`（label 在上、input 在下）；`.filters`（inline wrap，篩選列）。
- **狀態多選篩選**（orders_list 的 checkbox 清單）→ 樣式化成 `.chip`（膠囊切換鈕，選中變青綠 tint）。
- 按鈕：`.btn`（次級：surface 底 + 邊框）、`.btn--primary`（青綠實心 + `--on-accent` 深字，用於「查詢/更新/建立/送出」主動作）、`.btn--danger`（紅字邊框，用於刪除/登出等）、`.btn--sm`。
- **TOTP 驗證碼輸入** `.input--code`：等寬 + `letter-spacing` 加寬 + 置中，強化「這是驗證碼」的視覺提示（用於改判、參數容許誤差、簽名頁等每次要求 TOTP 的地方）。

### 5.5 狀態 pill

`.pill` 基底（膠囊、小字、前導小圓點）+ `.pill--<status>` 修飾（顏色見 2.3）。用於訂單狀態、歸集狀態、webhook 狀態、廣播狀態。

### 5.6 flash / alert

`.alert--error / --success / --info` 取代現有 inline `color:red/green/blue`，改為「左側語意色 + tinted 底 + 邊框」的區塊，暗底對比更清楚。

### 5.7 登入 / TOTP 極簡頁

無側邊欄，改為**置中卡片**（`.auth-card`），頂部品牌 mark，背景加一層極淡的青綠 radial 光暈。TOTP 驗證頁的 6 碼輸入用 `.input--code`。

---

## 6. 版面結構的實作考量（兩個 package 各自 ParseFS）

現況：`internal/admin` 與 `internal/consolidation` 各自 `template.Must(...).ParseFS(...)`、互不共用模板，兩份 `_nav.html` 內容目前是**各自維護的重複副本**；`internal/auth` 又有第三份較舊的 nav。改成側邊欄後：

- CSS 只有一支共用檔 `web/static/css/admin.css`，三個 package 皆 `<link>` 同一支——**視覺一致靠 CSS 集中，不受模板分屬兩 package 影響**。
- 但側邊欄 + 主內容需要外層 `<div class="layout"><aside class="sidebar">…</aside><main class="main">…內容…</main></div>` 包裹。建議做法（實作階段再定案，二選一）：
  - **(a)** nav 模板輸出「側邊欄 + `<main>` 起始標籤」，另一個 `_foot` 模板輸出「`</main>` + `localtime.js` + 收尾」，每頁首尾各叫一次；
  - **(b)** 每頁自行寫 `.layout/.main` 包裹，nav 模板只輸出 `<aside class="sidebar">`。
- 三份 nav（admin/consolidation/auth）仍需**逐份同步**（沿用目前「重複但同步」的現實），或藉此把 auth 那份較舊的 nav 一併對齊。

---

## 7. 必要的 CSP 變更（**實作階段才改，本設計只標註**）

因 Q3 選了自架 `@font-face` 字型檔，而兩個高風險頁目前 CSP 為：

```
default-src 'none'; script-src 'self'; connect-src 'self';
style-src 'self' 'unsafe-inline'; img-src 'self';
base-uri 'none'; form-action 'none'; frame-ancestors 'none'
```

- `style-src 'self' 'unsafe-inline'` → **外部 CSS 檔（同源 `<link>`）與 inline `<style>` 皆已可用，CSS 本身零阻礙**。
- `img-src 'self'` → 本地圖片可用；**inline SVG 圖示是 DOM 標記、不受 img-src 管制，永遠可用**。
- **但沒有 `font-src`** → `@font-face` 以 `url()` 載入本地 woff2 會回退到 `default-src 'none'` 而被擋。

**因此實作時需在下列兩檔的 CSP header 各加一段 `font-src 'self'`（僅同源，不加任何外部 host，不動 `script-src` 既有嚴格限制）：**

- `internal/admin/master_wallets_new.go`（常數 `masterWalletNewCSPHeader`，約第 19 行）
- `internal/consolidation/sign.go`（常數 `signCSPHeader`，約第 18 行）

改後：`... style-src 'self' 'unsafe-inline'; img-src 'self'; font-src 'self'; ...`。

> 這屬 CLAUDE.md 標示「改動高風險頁需人工確認」的範圍，故**本次不動程式碼**，只在此明列，讓你在實作階段知道會牽動這兩個檔的 security header。其餘後台頁面沒有這麼嚴格的 CSP，`font-src 'self'` 對它們無影響。
> 若日後想連 CJK 也自架字型，才需要更大的字型檔權衡，屬另一個獨立決策。

---

## 8. 分階段導入建議

| 階段 | 內容 | 風險 |
|---|---|---|
| **P0 地基** | 新增 `web/static/css/admin.css`（含全部 tokens 與元件）＋ `web/static/fonts/` 子集化 woff2；各模板 `<head>` 加 `<link rel="stylesheet" href="/static/css/admin.css">`；兩高風險頁 CSP 加 `font-src 'self'` | 低（純加法，暫不動版面即可先驗證配色） |
| **P1 側邊欄骨架** | 三份 `_nav.html` 改為側邊欄 + `.layout/.main` 包裹；決定第 6 節 (a)/(b) 包裹方式並套到所有頁 | 中（動全部模板外層結構，但不動業務邏輯） |
| **P2 高流量頁** | dashboard、orders_list、order_detail、audit_logs：表格樣式化、狀態 pill、統計磚、篩選 chip、alert | 中 |
| **P3 歸集流程** | consolidation_pending / wallets / address_book / fee_source_book、**consolidation_sign（高風險頁，逐行檢視、CSP 已於 P0 處理）** | 中高（含高風險頁） |
| **P4 設定頁** | master_wallets(_new)、api_keys(_created)、webhook_config、params、notifications、password | 中（master_wallet_new 為高風險頁） |
| **P5 認證極簡頁** | login、totp_verify、totp_setup、reverify（置中卡片版面） | 低 |

> 建議 P0 完成即可先讓你用真實系統確認「純配色地基」的觀感，再逐頁推進版面重做，避免一次大改難以回退。

---

## 9. 決策紀錄（本次會話拍板依據）

- **Q1 導覽**：(B) 側邊欄。
- **Q2 力度**：(B) 全面重做。
- **Q3 圖示/字體**：(C) 自訂 `@font-face` 字型檔（→ 第 7 節 CSP 變更需求）。
- **Q4 圖表**：(A) 本次不做，列未來獨立項目。
- **Q5 登入/TOTP 頁**：(A) 一併納入。
- **設定分組**：保留 **`<details>` 可折疊**（不改常展開）。
- **字型定案**：採用本文建議的 Inter + JetBrains Mono，使用者無意見。
- **表格 vs 卡片的界線**：同意本文主張——高密度列表（訂單列表/稽核日誌/入帳明細/待歸集列表）維持表格僅樣式化，dashboard 統計磚、錢包列表這類走卡片化。

全部取捨與次要項皆已拍板，下一步是排入實作（見「8. 分階段導入計畫」）。
