# UCollection API 串接文件

本文件說明商戶如何串接自己部署的 UCollection 服務：怎麼建立訂單、怎麼把付款人導向收銀頁、怎麼接收訂單狀態變化的 Webhook 通知。部署本身（怎麼把系統架起來）請見 `docs/部署文件.md`，本文件假設系統已經部署完成、可以正常存取後台。

## 一、前置準備

串接前需要先在後台完成兩件事：

1. **產生 API Key**：登入後台 → `/admin/api-keys` → 「重新產生」。畫面會顯示 `key` 與 `secret`，**僅此一次明碼顯示**，之後只會顯示前4碼+後4碼供識別，請立即複製保存。此操作每次都要求輸入當下的 TOTP 驗證碼。
2. **設定 Webhook**：登入後台 → `/admin/webhook-config`，填入你要接收通知的 URL（`http(s)://` 開頭），並產生或手動輸入一組 `webhook_secret`（跟 API Key 的 `secret` 是兩把不同的金鑰，不共用）。此操作同樣每次都要求輸入當下的 TOTP 驗證碼。

**重新產生 API Key 會立即讓舊金鑰失效**，沒有新舊並存的過渡期——正式環境如果要換金鑰，換完要立刻同步更新你的串接設定，否則呼叫 API 會馬上開始收到 401。Webhook secret 輪替同理：換了之後所有還沒送達的排隊通知會立刻改用新 secret 簽章，也要同步更新你這邊的驗簽設定。

設定完 Webhook URL 後，可以在同一頁按「測試」，系統會同步打一次 `event_type=TEST_PING` 的請求到你的端點，用來確認網路可達、簽章驗證邏輯正確——不需要真的建一筆訂單才能測試。**已知限制**：如果你的端點對 `event_type` 做嚴格白名單驗證，可能會拒絕這個測試專用的值，此時請改用回傳的 HTTP 狀態碼判斷即可。

## 二、串接流程總覽

1. 你的後端呼叫 `POST /api/v1/orders` 建立一筆訂單，取得系統分配的收款地址與收銀頁網址（`checkout_url`）。
2. 把付款人的瀏覽器導向 `checkout_url`——這頁完全由系統自己架設與維護（含 QR code、倒數、狀態顯示），你不需要另外開發付款畫面。
3. 系統在背景持續掃描鏈上事件，判斷這筆訂單的收款狀態。
4. 訂單進入最終狀態（COMPLETED / OVERPAID / CONFIRMATION_STALLED / EXPIRED 四者之一）時，系統會主動呼叫你設定的 Webhook URL 通知你。
5. 你也可以隨時用 `GET /api/v1/orders/{id}` 或 `GET /api/v1/orders?merchant_order_no=` 主動查詢目前狀態，作為 Webhook 之外的備援手段——建議只針對「訂單已超過 `expires_at` 一段時間、但你還沒收到對應終態 Webhook」的少數訂單低頻補查（例如每幾分鐘一次），不需要對所有訂單持續高頻輪詢。

這些 API 是設計給**你的後端對後端呼叫**，不是給瀏覽器前端直接呼叫：伺服器端沒有設定任何 CORS 允許跨域，瀏覽器直接呼叫會被瀏覽器擋下；`X-API-Key`/`secret` 也不應該出現在前端程式碼中，一旦外洩等同任何人都能用你的身分建單。

## 三、認證：API Key + HMAC 簽章

**這些請求務必全程走 HTTPS。** `X-API-Key` 是明碼傳輸，系統本身不內建 TLS（見 `docs/部署文件.md` 第八節），如果你這端還沒架好 HTTPS 反向代理就先用 http 呼叫正式環境，API Key 會在傳輸過程中明碼外洩。

所有 `/api/v1/...` 請求都要帶三個 Header：

| Header | 說明 |
|---|---|
| `X-API-Key` | 後台產生的 `key`，明碼 |
| `X-Timestamp` | 目前 Unix 時間戳（秒） |
| `X-Signature` | 見下方簽章方式 |

**簽章方式**：

```
待簽字串 = X-Timestamp值 + HTTP方法 + 請求路徑(含query string，跟實際送出的完全一致) + 請求body(GET請求視為空字串)
X-Signature = hex( HMAC-SHA256(secret, 待簽字串) )
```

幾個容易出錯的地方：

- 待簽字串是**直接字串相接**，中間沒有任何分隔符號。
- 路徑要包含 query string（例如 `/api/v1/orders?merchant_order_no=ORDER-0001`），且必須跟實際 HTTP 請求送出的位元組完全一致——先組好 query string 再簽章，不要簽完之後才讓 HTTP 函式庫重新編碼/排序參數，否則簽章會對不上。
- `X-Timestamp` 與伺服器當下時間相差超過 ±5 分鐘會被拒絕（防重放），所以每次請求都要用當下時間重新產生，不能快取舊的簽章重複使用。
- 認證失敗（金鑰不存在、金鑰已撤銷、簽章不符、時間戳過期）一律回同一種錯誤 `401 INVALID_SIGNATURE`，不會告訴你確切原因是什麼——這是刻意設計，避免外部藉由錯誤訊息差異探測金鑰是否存在，串接時如果一直收到這個錯誤，請照上面幾點逐一排查。判斷依據：**400 是請求格式問題（缺欄位、body 太大、JSON 格式錯），401 是金鑰/簽章/時間戳問題**——但要注意 POST 請求的 JSON 格式檢查發生在簽章驗證「之後」，所以格式錯的 POST 請求如果簽章也是錯的，你只會看到 401，不會看到 400。另外請求 body 上限 1MB，超過會被直接截斷讀取，導致簽章怎麼算都對不上、一樣回 401。
- 如果 `merchant_order_no` 含有中文、空白、`&`、`+` 等需要 URL-encode 的字元（欄位本身沒有格式限制，純文字皆可），**GET 查詢時務必先把它 encode 成 query string，再對 encode 後的完整路徑+query string 簽章**——簽章是對「實際送出的位元組」算的，如果你簽的是 encode 前的原始字串，送出去的請求會對不上簽章而回 401。建單時放在 JSON body 裡就不需要額外處理，body 本身就是簽章的一部分。

### curl 範例：建立訂單

```bash
# 注意：變數名不要用 PATH——那是 shell 保留的環境變數，
# 覆蓋掉之後 openssl/curl 這些外部指令會找不到路徑而執行失敗。
API_KEY="你的key"
API_SECRET="你的secret"
TIMESTAMP=$(date +%s)
METHOD="POST"
REQ_PATH="/api/v1/orders"
BODY='{"merchant_order_no":"ORDER-0001","target_amount":"100000000"}'

STRING_TO_SIGN="${TIMESTAMP}${METHOD}${REQ_PATH}${BODY}"
SIGNATURE=$(printf '%s' "$STRING_TO_SIGN" | openssl dgst -sha256 -hmac "$API_SECRET" | sed 's/^.* //')

curl -X POST "https://pay.merchant.example${REQ_PATH}" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ${API_KEY}" \
  -H "X-Timestamp: ${TIMESTAMP}" \
  -H "X-Signature: ${SIGNATURE}" \
  -d "$BODY"
```

### curl 範例：依訂單編號查詢

```bash
TIMESTAMP=$(date +%s)
METHOD="GET"
REQ_PATH="/api/v1/orders?merchant_order_no=ORDER-0001"

STRING_TO_SIGN="${TIMESTAMP}${METHOD}${REQ_PATH}"
SIGNATURE=$(printf '%s' "$STRING_TO_SIGN" | openssl dgst -sha256 -hmac "$API_SECRET" | sed 's/^.* //')

curl -X GET "https://pay.merchant.example${REQ_PATH}" \
  -H "X-API-Key: ${API_KEY}" \
  -H "X-Timestamp: ${TIMESTAMP}" \
  -H "X-Signature: ${SIGNATURE}"
```

（GET 請求沒有 body，待簽字串到路徑為止，後面等同接一個空字串。）

若 `merchant_order_no` 含需要 encode 的字元，例如訂單編號是 `訂單 0001`，要先 encode 成 query string 再簽章：

```bash
REQ_PATH="/api/v1/orders?merchant_order_no=%E8%A8%82%E5%96%AE%200001"
# STRING_TO_SIGN 用這個 encode 後的 REQ_PATH 組，curl 也要用同一個 REQ_PATH 發送，
# 兩邊字元必須完全一致，否則簽章對不上。
```

## 四、API 端點

### 金額單位

所有金額欄位都是 **USDT 最小單位（6位小數）的整數字串**，不是浮點數。例如 100 USDT 要傳 `"100000000"`；不要傳 `"100.5"` 這種小數形式。`target_amount` 上限為 int64 可表示範圍（9223372036854775807），超過會直接回 `400 INVALID_REQUEST`，實務上不會碰到這個限制。

### `POST /api/v1/orders` — 建立訂單

Request body：

```json
{
  "merchant_order_no": "string，你自己的訂單編號，必填",
  "target_amount": "string，USDT最小單位整數字串，必填"
}
```

Response（`201 Created`，或冪等重放時 `200 OK`）：

```json
{
  "id": 123,
  "merchant_order_no": "ORDER-0001",
  "address": "T...",
  "target_amount": "100000000",
  "status": "PENDING",
  "expires_at": "2026-09-17T12:00:00Z",
  "checkout_url": "https://pay.merchant.example/checkout/xxxxxxxx"
}
```

**冪等性規則**：`merchant_order_no` 相同、`target_amount` 也相同 → 視為網路逾時重試，直接回傳原本那筆訂單（`200`），**不會**建立新訂單或分配新地址；`merchant_order_no` 相同但 `target_amount` 不同 → 視為衝突，回 `409 ORDER_CONFLICT`。這代表你這端如果請求逾時不確定有沒有成功，可以直接用同一組 `merchant_order_no`+`target_amount` 安全地重試，不會造成重複建單。不同的 `merchant_order_no` 各自獨立，互不影響，即使金額相同也會各自建立、各自分配獨立收款地址。

**訂單一經建立，`target_amount` 就不能修改**——沒有提供更新金額的 API，上面的 409 衝突規則就是防止你誤用同一個 `merchant_order_no` 改金額。如果訂單金額真的要改，請用新的 `merchant_order_no` 另外建一筆。

`address` 每筆訂單都不同：系統依 xpub（HD 錢包公鑰）為每筆訂單各自衍生一個獨立收款地址，地址跟訂單永久一對一綁定，不會重複使用，你不需要（也不應該）把多筆訂單導向同一個地址收款。

### `GET /api/v1/orders/{id}` — 依系統訂單 ID 查詢

### `GET /api/v1/orders?merchant_order_no={你的訂單編號}` — 依你的訂單編號查詢

兩者回傳格式相同：

```json
{
  "id": 123,
  "merchant_order_no": "ORDER-0001",
  "address": "T...",
  "status": "COMPLETED",
  "target_amount": "100000000",
  "detected_amount": "100000000",
  "confirmed_amount": "100000000",
  "expires_at": "2026-09-17T12:00:00Z",
  "transitions": [
    {
      "from_status": "PENDING",
      "to_status": "CONFIRMING",
      "changed_by": "system",
      "note": "",
      "created_at": "2026-09-17T11:50:00Z"
    }
  ]
}
```

- `detected_amount`：鏈上偵測到、但不論是否已達最終確定的累計金額。
- `confirmed_amount`：鏈上已達最終確定（solidified）的累計金額，只有這個數字才代表真正入帳確認。
- `transitions`：這筆訂單完整的狀態變更歷史，包含系統自動判斷與人工改判紀錄，append-only（不會被覆蓋或刪除）。

只支援單筆查詢，沒有列表/分頁功能；列表查詢是後台管理介面的功能，不在 API 範疇內。

### 訂單狀態值

| 狀態 | 說明 |
|---|---|
| `PENDING` | 已建立，等待付款 |
| `CONFIRMING` | 已偵測到入帳，等待鏈上最終確定 |
| `COMPLETED` | 已完成（終態） |
| `OVERPAID` | 溢付完成（終態） |
| `CONFIRMATION_STALLED` | 確認遲滯（終態，需人工查證） |
| `EXPIRED` | 已過期（終態） |

四個終態各自會觸發一次 Webhook 通知（見下一節）。

**`COMPLETED` 不保證收到足額 `target_amount`。** 系統依後台設定的「容許誤差百分比」，在建單當下算出一個金額區間 `[下界, 上界]`：鏈上最終確認金額只要落在這個區間內就會轉 `COMPLETED`（即使略低於 `target_amount`，也就是短收在容許範圍內一樣算完成）；超過上界才轉 `OVERPAID`；低於下界則不會轉態，繼續等待或最終進入 `CONFIRMATION_STALLED`。**這是財務對帳的關鍵點：請一律以查詢 API 回傳的 `confirmed_amount`（或 Webhook 的 `total_confirmed_amount`）作為實際入帳金額，不要假設 `COMPLETED` 就等於收到了 `target_amount`。** 容許誤差百分比是商戶自己在後台設定的值，此 API 不會告訴你目前設定是多少。

**`EXPIRED` 是系統背景輪詢判定，不是到了 `expires_at` 那一刻精確轉態**，實際轉態時間可能比 `expires_at` 晚幾秒到幾十秒（視背景任務輪詢間隔而定）。如果付款發生在 `expires_at` 前後的臨界時間點，只要系統在轉為 `EXPIRED` 之前先偵測到入帳，訂單就會正常走 `CONFIRMING` 而不會被判為過期——請不要在 `expires_at` 一到就片面認定訂單已失效，仍以實際回傳的 `status` 為準。

### 錯誤回應格式

統一格式：

```json
{"error_code": "string", "message": "string"}
```

| HTTP 狀態 | error_code | 說明 |
|---|---|---|
| 400 | `INVALID_REQUEST` | 請求格式錯誤（缺欄位、金額格式不對等） |
| 401 | `INVALID_SIGNATURE` | 認證失敗（見上一節，不區分細節原因） |
| 404 | `ORDER_NOT_FOUND` | 查無此訂單 |
| 409 | `ORDER_CONFLICT` | `merchant_order_no` 已存在但金額不同 |
| 500 | `PARAMS_NOT_CONFIGURED` | 商戶尚未在後台完成訂單參數設定（有效期/容許誤差），需先請商戶後台管理員完成設定才能建單 |
| 500 | `INTERNAL_ERROR` | 系統內部錯誤 |
| 503 | `NO_ACTIVE_MASTER_WALLET` | 尚未設定啟用中的代收主錢包，需先請商戶後台管理員完成設定 |

### Rate limiting

目前應用層本身沒有實作速率限制，這部分交由商戶自行架設的反向代理層（nginx/Caddy 等，見 `docs/部署文件.md` 第八節）視需要處理。

## 五、付款頁（收銀台）

`checkout_url` 是系統自己架設、維護的完整付款頁面（QR code、收款地址、倒數計時、即時狀態），**你不需要自己開發這頁**，把付款人的瀏覽器導向這個網址即可。頁面內部每 5 秒自動輪詢一次最新狀態，訂單進入終態後自動停止輪詢。

## 六、接收 Webhook 通知

### 觸發時機

訂單進入四個終態之一（`COMPLETED`／`OVERPAID`／`CONFIRMATION_STALLED`／`EXPIRED`）時，系統會各觸發一次通知。**同一筆訂單有可能收到不只一次通知**：例如系統先判定 `CONFIRMATION_STALLED` 並通知你，之後商戶後台管理員人工查證後改判為 `COMPLETED`，這又會是新的一次終態進入、再觸發一次通知。請依 `event_type`/`status` 判斷這是哪一次的通知，不要假設每筆訂單只會收到一次。

**Webhook 不是即時推播。** 系統用背景 worker 每 10 秒巡一次待送清單，訂單進終態到第一次真正送出通知之間，本來就會有數秒的延遲；如果第一次沒送成功還會再等重試間隔。請不要設計成「終態發生的瞬間就必須收到通知」的邏輯，時間敏感的場景建議搭配上一節提到的 GET 查詢備援。

### Payload

```json
{
  "event_id": "隨機字串，用來去重",
  "event_type": "ORDER_COMPLETED | ORDER_OVERPAID | ORDER_CONFIRMATION_STALLED | ORDER_EXPIRED",
  "order_id": "123",
  "merchant_order_no": "ORDER-0001",
  "status": "COMPLETED",
  "target_amount": "100000000",
  "total_confirmed_amount": "100000000",
  "occurred_at": "2026-09-17T12:00:00Z"
}
```

金額欄位跟 API 一樣是字串形式的最小單位整數。

### 驗簽

Header 帶 `X-Webhook-Timestamp`（Unix秒字串）與 `X-Webhook-Signature`：

```
X-Webhook-Signature = hex( HMAC-SHA256(webhook_secret, X-Webhook-Timestamp值 + 原始body位元組) )
```

跟 API 認證用同一套 HMAC-SHA256 邏輯，但只有「時間戳+body」兩段直接相接（不含 method/path），且用的是後台設定的 `webhook_secret`——跟 API Key 的 `secret` 是不同的金鑰，不要搞混。驗簽時務必用**收到的原始 body 位元組**去算，不要先反序列化成物件再重新序列化，否則欄位順序/空白差異會導致簽章對不上。

### 你的端點需要做到

- **回應 2xx** 才會被視為成功。任何非 2xx（含 3xx 轉址——系統**不會**跟隨轉址，會直接當作失敗）都會觸發重試。
- **依 `event_id` 做去重**：系統是 at-least-once 語意，同一個 event 有可能因為系統重啟等情況被重複送達，不保證恰好一次。你的處理邏輯需要能安全地處理同一個 `event_id` 收到兩次以上的情況。
- 逾時：系統端等待回應最多 10 秒。

### 重試策略

失敗後採指數退避重試：第一次失敗後 1 分鐘重試，之後每次延遲翻倍，上限 6 小時；從第一次自動嘗試起算 24 小時內都會持續重試，超過這個時間窗仍未成功就標記為最終失敗、不再自動重試。如果最終失敗，商戶後台管理員可以在後台手動重發（不影響你的處理邏輯，你這端看到的仍然是同一份 payload）。
