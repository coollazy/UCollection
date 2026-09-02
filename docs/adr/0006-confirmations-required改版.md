# ADR-0006：拿掉商戶可調「確認數」，改固定沿用 Tron 協定 solidified 狀態

- Status: Accepted
- Date: 2026-09-01

## 背景

原始需求書設計「確認等待逾時時間」旁邊還有一個商戶可調的「確認數」參數（`confirmations_required`），訂單需要累積夠多的確認數才算最終確定。技術架構設計討論模組4（鏈上監控）時發現，「確認數達標後再加一段安全緩衝期才停止追蹤 reorg」這個設計把「商戶自訂確認數」跟「Tron 協定自身 solidified 狀態」當成同一件事，但緩衝期該加多久講不出根據——這正是當時 Gate 卡住的其中一個卡點。

## 決策

**移除商戶可調的「確認數」參數**，訂單是否達最終確定，一律直接沿用 TronGrid `only_confirmed=true` 這個布林結果（Tron 協定自身認定的 solidified 狀態），不再由系統自行計算「目前區塊高度－交易區塊高度」跟商戶設定值比較。

## 理由

- Tron 協定本身已經定義「solidified」狀態，`only_confirmed=true` 查詢命中即代表協定自身認定不可逆，系統不需要在協定之上再疊加一層自訂數字判斷。
- 移除後，`incoming_transactions` 的 reorg 檢查範圍自然限縮為「`only_confirmed=false` 已命中、尚未查到 `only_confirmed=true`」這段期間，查到 `only_confirmed=true` 後即停止追蹤，不需要再額外加緩衝期——這個緩衝期在舊設計裡講不出根據，新設計裡這個問題直接消失。
- 連帶解決「COMPLETED 後又被 reorg 打回」這個原本需要模組5額外定義的路徑：達到 COMPLETED/OVERPAID 的當下即等同協定認定不可逆，定義上不會再發生。

## 後果（連動修訂）

- 需求書 5.2/5.5：移除確認數相關文字。
- 技術架構設計第2節 schema：`orders` 表參數快照欄位從四個減為三個，`incoming_transactions` 表 `confirmations`（數字）欄位改為 `confirmed`（boolean）；`system_params` 全域參數清單移除確認數。
- 技術架構設計第4節：任務3更名為「最終確定（solidified）判定＋reorg檢查」，判斷邏輯改為直接以 `only_confirmed=true` 查詢。
- 技術架構設計第5節（訂單狀態機模組）：因此決策而正式定案，狀態轉換判斷條件據此拆分為兩條查詢（PENDING→CONFIRMING 看 `only_confirmed=false` 累加、CONFIRMING→COMPLETED/OVERPAID 看 `confirmed=true` 累加）。

## 相關文件

- [需求書變更紀錄 v0.36](../開發流程框架-01-需求書變更紀錄.md#v0-36)
- [技術架構設計變更紀錄 v0.5](../開發流程框架-03-技術架構設計變更紀錄.md#v0-5)
- [技術架構設計.md 第4節](../開發流程框架-03-技術架構設計.md#4-鏈上監控模組internalscanner)、[第5節](../開發流程框架-03-技術架構設計.md#5-訂單狀態機模組internalorder)
