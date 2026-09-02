# ADR-0011：密碼+TOTP 兩階段登入與 RequireFreshTOTP step-up 驗證

- Status: Accepted
- Date: 2026-09-02

## 背景

需求書 5.5 要求單一管理帳號、強制 2FA（TOTP）、不提供應用層級密碼/2FA 救援機制。技術架構設計草案初版第一輪 subagent 審查發現 `RequireSession` middleware 只檢查時間條件、未檢查 `status` 欄位，`pending_2fa`（僅通過密碼、尚未完成TOTP）的 session 若直接打其他 `/admin/...` 路由會被誤判通過，等於2FA可被繞過。

## 決策

1. **登入分兩階段**：`POST /admin/login` 驗證密碼成功後建立 `status='pending_2fa'` 的暫時 session（5分鐘過期）；`POST /admin/login/totp` 驗證 TOTP 通過後才轉為 `status='active'`（12小時絕對過期）。`RequireSession` middleware 明確要求 `status='active'` **且**未過期**且**30分鐘內活動，三者皆成立才放行。
2. **高風險操作（`/tron-proxy/...`、手動改判、代收主錢包切換、API Key重新產生、Webhook secret/URL變更）額外要求 `RequireFreshTOTP(15分鐘)`**：即使一般 session 有效，若距上次 TOTP 驗證超過15分鐘，需重新輸入 TOTP 碼（`POST /admin/reverify-totp`）才能繼續。
3. **暴力破解防禦雙層疊加**：IP層級（15分鐘滑動視窗，失敗超過10次鎖定該IP）+ per-session層級（`totp_attempt_count` 連續失敗5次即整個session失效，任一次驗證成功即歸零）。刻意不做帳號層級永久鎖定（唯一帳號名稱不算秘密，帳號級鎖定等於任何人持續嘗試錯誤密碼就能讓商戶自己也登不進後台，構成阻斷服務攻擊面）。

## 理由

- **這兩項是需求書字面之外，本模組主動加上的防禦深度**，已經使用者確認採用：
  - `/tron-proxy` 等高風險操作要求15分鐘 TOTP 新鮮度：手動歸集涉及實際資金轉出，風險等級比一般後台瀏覽操作高一個量級；若後台 session cookie 遭竊但2FA裝置未同時失竊，此機制讓攻擊者仍卡在需要即時TOTP碼這一關，30分鐘閒置逾時本身不足以涵蓋「session存活期間但已離開太久沒盯著」的情境。
  - 同一session連續5次TOTP驗證失敗即強制登出（即使仍在12小時效期內）：讓「已知正確密碼、只是在猜TOTP碼」的攻擊者無法無限次嘗試，疊加IP限流後即使換IP也受per-session計數約束。
- **威脅模型邊界要明講，避免被誤解為萬用防線**：`RequireFreshTOTP` 設計目標是防禦「session cookie/token外洩、但2FA裝置未同時外洩」這類情境；若攻擊面是管理者瀏覽器本身已淪陷（例如惡意瀏覽器擴充套件即時劫持管理者剛完成的TOTP驗證狀態），此機制無法提供有效保護，屬商戶自身裝置資安責任，不在本模組防護範圍內。

## 已知取捨（刻意接受，不是遺漏）

- **`last_totp_step` 帳號層級重放防護的雙分頁假陽性**：單一帳號同時開兩個分頁各自需要TOTP驗證，把手機上同一組當前碼分別貼進兩邊，較晚送達的請求會被判定為「已被使用」，須等待下一組新碼（最多30秒）。這是帳號層級單一 `last_totp_step` 換取「防止密碼外洩後code被另一個不同session重放」這個真正安全目的的必然代價，不計入嘗試次數、有明確錯誤訊息，判定可接受。
- **per-session失效機制的騷擾面**：攻擊者僅取得某個 active session的cookie（未取得2FA裝置），可對該session連續送5次錯誤`/admin/reverify-totp`觸發它被強制刪除，造成管理者被迫重新完整登入一次的騷擾。這不會讓攻擊者取得任何實質存取權限，純屬可用性層面滋擾，判定可接受，不額外設計對策。

## 相關文件

- [需求書.md 5.5「帳號安全」](../開發流程框架-01-需求書.md#55-後台商戶管理介面)
- [技術架構設計.md 第9節](../開發流程框架-03-技術架構設計.md#9-帳號安全模組internalauth)
- [技術架構設計變更紀錄 v0.10](../開發流程框架-03-技術架構設計變更紀錄.md#v0-10)
