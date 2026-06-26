export default {
  "paidSub": {
    "experimental": "實驗性",
    "secretboxWarning": "在正式環境中，請設定 SUI_SECRETBOX_KEY 環境變數，以便使用保存在資料庫之外的金鑰加密付款權杖。",
    "tabs": {
      "bindings": "綁定", "autoreg": "自動註冊", "tariffs": "方案",
      "payments": "付款", "messages": "訊息", "orders": "訂單", "bot": "機器人"
    },
    "active": "啟用",
    "disabled": "停用",
    "cols": {
      "client": "用戶端", "clientId": "用戶端 ID", "description": "說明",
      "telegramId": "Telegram ID", "expiry": "到期時間", "status": "狀態",
      "name": "名稱", "price": "價格", "stars": "Stars", "addDays": "+天",
      "addTraffic": "+流量", "enabled": "已啟用", "id": "ID",
      "clientName": "用戶端名稱", "provider": "付款服務商", "amount": "金額", "created": "建立時間"
    },
    "bindings": {
      "hint": "將面板用戶端綁定到 Telegram 使用者 ID。綁定後，用戶端可在機器人中取得連結、QR Code、統計資料並完成付款。",
      "add": "新增綁定",
      "empty": "找不到用戶端。請在用戶端頁面建立用戶端（或啟用自動註冊），然後在此綁定 Telegram ID。",
      "none": "沒有綁定"
    },
    "autoreg": {
      "enable": "自動註冊未知使用者", "inbounds": "新用戶端使用的入站",
      "trialDays": "試用天數", "trialVolume": "試用流量（GB，0 = 不限量）",
      "maxClients": "自動註冊用戶端上限", "rateLimit": "/start 頻率限制（每分鐘）"
    },
    "tariffs": {
      "add": "新增方案", "none": "沒有方案", "new": "新方案", "edit": "編輯方案",
      "priceMajor": "價格（主要貨幣單位）", "currency": "貨幣", "starsAmount": "Stars 數量（XTR）",
      "addDays": "+天", "addTrafficGB": "+流量（GB，0 = 不限量）", "sort": "排序", "enabledField": "啟用"
    },
    "payments": {
      "currency": "預設貨幣（例如 RUB、USD）", "orderTtl": "待付款訂單有效期（分鐘）",
      "stars": "Telegram Stars (XTR)", "yookassa": "YooKassa", "yookassaToken": "YooKassa provider_token（BotFather）",
      "stripe": "Stripe", "stripeToken": "Stripe provider_token（BotFather）",
      "paymaster": "PayMaster", "paymasterToken": "PayMaster provider_token（BotFather）",
      "crypto": "CryptoBot", "cryptoToken": "CryptoBot API 權杖", "external": "外部付款連結",
      "externalTemplate": "外部 URL 範本（https://…，可使用 {'{'}orderId{'}'} {'{'}amount{'}'} {'{'}currency{'}'} {'{'}clientId{'}'}）"
    },
    "messages": {
      "greetingTitle": "/start 歡迎語", "greetingHint": "已綁定用戶端開啟機器人時顯示。留空則使用預設歡迎語。",
      "greetingLabel": "自訂歡迎語", "broadcastTitle": "向所有用戶端群發",
      "broadcastHint": "向所有已綁定的 Telegram 使用者傳送一次性公告（{count} 位接收者）。",
      "broadcastLabel": "公告內容", "sendAll": "傳送給所有人", "result": "已傳送：{sent} · 失敗：{failed}",
      "confirmTitle": "傳送公告？", "confirmText": "訊息將傳送給全部 {count} 個已綁定用戶端。", "send": "傳送"
    },
    "orders": { "refund": "退款" },
    "refund": {
      "title": "為訂單 #{id} 退款？", "starsNote": "Telegram Stars 將自動退還。",
      "manualNote": "這裡只會將訂單標記為已退款；請在付款服務商後台完成退款。",
      "revoke": "撤銷此訂單授予的天數/流量", "done": "退款已處理"
    },
    "bot": {
      "enable": "啟用用戶端機器人", "pollTimeout": "長輪詢逾時（秒）",
      "token": "機器人權杖（獨立於管理員機器人）", "transportTitle": "連線 / 傳輸",
      "transportHint": "此機器人連線 Telegram 的方式，獨立於管理員 Telegram 模組。",
      "transport": "傳輸", "outbound": "出站（sing-box）— 需要核心正在執行",
      "noOutbounds": "尚未設定出站", "proxyUrl": "代理 URL（http/https/socks5，留空 = 直接連線）",
      "proxyUser": "代理使用者名稱（選填）", "proxyPass": "代理密碼（選填）"
    },
    "bindingDialog": {
      "addTitle": "新增綁定", "editTitle": "將 Telegram 綁定到 {name}",
      "client": "用戶端", "tgId": "Telegram 使用者 ID（0 = 解除綁定）"
    },
    "unbind": {
      "title": "解除 {name} 的 Telegram 綁定？",
      "text": "這會移除用戶端 #{clientId} 的 Telegram 關聯（Telegram ID {tgUserId}）。用戶端仍保留在面板中，只會清除綁定。",
      "confirm": "解除綁定"
    },
    "transportModes": { "proxy": "代理", "outbound": "出站（sing-box）" }
  }
}
