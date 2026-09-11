# ADR-0018：手動歸集「準備歸集」與簽名頁 GET 不再套用 TOTP，驗證收斂到簽名頁本身（部分修訂ADR-0017）

- Status: Accepted
- Date: 2026-09-11

## 背景

ADR-0017 落地後，使用者在 8080 實測 Phase 4b（準備歸集→整合簽名頁）時發現：完整走一次流程要輸入 TOTP 驗證碼最多三次——

1. `POST /admin/consolidation/prepare-flow`（準備歸集送出，疊加 `RequireTOTPCode`）。
2. `GET /admin/consolidation/sign`（簽名頁本身，依技術架構設計第10節「頁面權限分級」表疊加 `RequireTOTPCode`；GET 沒有欄位可帶驗證碼，一律先導向 `/admin/reverify-totp` 要求輸入）。
3. 簽名頁自己的「TOTP驗證碼（整批只需輸入一次）」欄位，由第一次 `/tron-proxy/.../prepare` 呼叫消耗。

使用者提出：準備歸集不需要 TOTP，驗證應該只留在簽名頁那一次。

## 決策

**`POST /admin/consolidation/prepare-flow` 與 `GET /admin/consolidation/sign` 改回單純 `RequireSession`，不疊加 `RequireTOTPCode`。** TOTP 驗證完全收斂到簽名頁本身既有的「TOTP驗證碼（整批只需輸入一次）」欄位，由 `page.js` 讀取後隨第一次 `/tron-proxy/{consolidation,fee-topup}/prepare` 呼叫送出（`RequireTOTPCodeOrRecentStepUp`，行為不變），批次寬限窗（30分鐘，ADR-0017決策8）也不變。

一併把「每筆金額（TRX）」欄位從待歸集頁拿掉，改由 `createFlowHandler` 送出時自動呼叫試算邏輯（與 `POST /admin/consolidation/fee-estimate` 共用同一段 TronGrid 模擬）算出首筆/其餘筆兩個金額，帶在 redirect 查詢字串傳給簽名頁（唯讀顯示，不可編輯）。此為連帶簡化，不涉及安全模型變動——`fee-estimate` 端點本來就是唯讀 `RequireSession`，現在只是把它在送出當下自動呼叫一次，省去商戶手動按「試算」再抄一次數字的步驟。

## 理由與被否決方案

- **維持三道TOTP關卡（被否決）**：最保守，但實務上每完成一次準備歸集就要連續輸入三次驗證碼（含一次被動彈出的 reverify 頁面），對一個原本就是為了「精簡商戶操作步驟」而做的整合流程（ADR-0017 的核心目標）是直接的體驗矛盾。
- **拿掉這兩處TOTP的殘留風險**：準備歸集本身不搬錢，只建立 `consolidation_batches`/`fee_topup_batches` 兩筆記錄（目的地/來源地址、選中的訂單）。若攻擊者僅取得 session cookie（未取得2FA裝置），理論上仍能呼叫這條路由建立一批「目的地地址被竄改」的待簽名批次——但無法讓任何資金真正移動：`/tron-proxy/.../prepare`、`/broadcast` 仍受 `RequireTOTPCodeOrRecentStepUp` 把關，攻擊者沒有當下有效的TOTP碼就無法讓這批東西被簽名廣播。真正的剩餘風險是「商戶自己不知情地簽名並廣播了一筆被竄改過目的地的批次」——這需要攻擊者先讓商戶造訪一個帶著特定 `consolidation_batch_id`/`fee_topup_batch_id` 的連結（類似 CSRF 誘導點擊），且商戶完全沒注意簽名頁上明確列出的「目的地地址：...　TRX來源：...」摘要列（安全鐵律4自我核對精神的延伸）就直接簽名送出。經與使用者說明此風險後，**使用者確認接受**：正常操作路徑下，準備歸集是商戶自己在待歸集頁勾選/填寫後立即送出，不會透過外部連結進入；簽名頁本身仍會顯示待核對的目的地/來源摘要，商戶在輸入助記詞前有機會核對。
- **只拿掉其中一處（例如只拿掉GET /sign的reverify，保留prepare-flow的TOTP，被否決）**：若只拿掉一邊，商戶仍要在「準備歸集」跟「簽名頁」分別輸入一次，沒有真正解決「輸入太多次」的抱怨；且兩邊各自防護的資產相同（都只是決定batch內容、不移動資金），沒有理由差別對待。

## 後果（連動修訂）

- 技術架構設計第10節「頁面權限分級」表：`GET /admin/consolidation/sign` 一列驗證方式由 `RequireSession + RequireTOTPCode` 改為 `RequireSession`；新增 `POST /admin/consolidation/prepare-flow` 一列（`RequireSession`，唯一不疊加TOTP的「起手式」寫入路由，理由同本ADR）。
- CLAUDE.md 安全鐵律9：不需修改——該條原文列舉的高風險操作清單本來就沒有明列「手動歸集準備頁/簽名頁GET」，這是模組內部（技術架構設計第10節）自行拍板的細項，不屬於CLAUDE.md鐵律文字本身的範圍。
- 程式碼：`internal/consolidation/routes.go`（`prepare-flow`/`GET .../sign` 改用 `requireSession`）、`batch_handlers.go`（`createFlowHandler` doc comment 補完整理由）、`estimate.go`（萃取 `estimateConsolidationFeesSun` 供 `createFlowHandler` 與 `feeEstimateHandler` 共用）、`sign.go`（`first_amount_per_order`/`repeat_amount_per_order` 取代單一 `amount_per_order`）、`web/js-src/page.js`（拿掉可編輯金額欄位，改唯讀顯示）、對應測試改寫（見 `docs/進度.md` 2026-09-11 交接筆記）。

## 相關文件

- [ADR-0017](0017-手動歸集流程重構自動編排與精省手續費.md)（本 ADR 部分修訂其簽名頁/準備歸集的 TOTP 套用方式，其餘決策不變）
- [ADR-0016](0016-高風險操作每次要求TOTP不設新鮮度窗口.md)（`RequireTOTPCodeOrRecentStepUp` 批次寬限機制維持不變）
- 技術架構設計第10節「頁面權限分級」、`docs/進度.md`（2026-09-11 交接筆記）
