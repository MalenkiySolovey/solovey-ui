export default {
  "paidSub": {
    "experimental": "experimental",
    "secretboxWarning": "For production, set the SUI_SECRETBOX_KEY environment variable so payment tokens are encrypted with a key kept outside the database.",
    "tabs": {
      "bindings": "Bindings",
      "autoreg": "Auto-registration",
      "tariffs": "Tariffs",
      "payments": "Payments",
      "messages": "Messages",
      "orders": "Orders",
      "bot": "Bot"
    },
    "active": "active",
    "disabled": "disabled",
    "cols": {
      "client": "Client",
      "clientId": "Client ID",
      "description": "Description",
      "telegramId": "Telegram ID",
      "expiry": "Expiry",
      "status": "Status",
      "name": "Name",
      "price": "Price",
      "stars": "Stars",
      "addDays": "+Days",
      "addTraffic": "+Traffic",
      "enabled": "Enabled",
      "id": "ID",
      "clientName": "Client Name",
      "provider": "Provider",
      "amount": "Amount",
      "created": "Created"
    },
    "bindings": {
      "hint": "Bind a panel client to a Telegram user ID. The client then gets links/QR/stats and can pay in the bot.",
      "add": "Add binding",
      "empty": "No clients found. Create a client on the Clients page (or enable Auto-registration), then bind it to a Telegram ID here.",
      "none": "No bindings"
    },
    "autoreg": {
      "enable": "Auto-register unknown users",
      "inbounds": "Inbounds for new clients",
      "trialDays": "Trial days",
      "trialVolume": "Trial traffic (GB, 0 = unlimited)",
      "maxClients": "Max auto-registered clients",
      "rateLimit": "/start rate limit (per min)"
    },
    "tariffs": {
      "add": "Add tariff",
      "none": "No tariffs",
      "new": "New tariff",
      "edit": "Edit tariff",
      "priceMajor": "Price (major units)",
      "currency": "Currency",
      "starsAmount": "Stars amount (XTR)",
      "addDays": "+Days",
      "addTrafficGB": "+Traffic (GB, 0 = unlimited)",
      "sort": "Sort",
      "enabledField": "Enabled"
    },
    "payments": {
      "currency": "Default currency (e.g. RUB, USD)",
      "orderTtl": "Pending order TTL (min)",
      "stars": "Telegram Stars (XTR)",
      "yookassa": "YooKassa",
      "yookassaToken": "YooKassa provider_token (BotFather)",
      "stripe": "Stripe",
      "stripeToken": "Stripe provider_token (BotFather)",
      "paymaster": "PayMaster",
      "paymasterToken": "PayMaster provider_token (BotFather)",
      "crypto": "CryptoBot",
      "cryptoToken": "CryptoBot API token",
      "external": "External payment link",
      "externalTemplate": "External URL template (https://… with {'{'}orderId{'}'} {'{'}amount{'}'} {'{'}currency{'}'} {'{'}clientId{'}'})"
    },
    "messages": {
      "greetingTitle": "Greeting on /start",
      "greetingHint": "Shown to a bound client when they open the bot. Leave empty for the default greeting.",
      "greetingLabel": "Custom greeting",
      "broadcastTitle": "Broadcast to all clients",
      "broadcastHint": "Sends a one-off announcement to every bound Telegram user ({count} recipient(s)).",
      "broadcastLabel": "Announcement text",
      "sendAll": "Send to all",
      "result": "Sent: {sent} · failed: {failed}",
      "confirmTitle": "Send announcement?",
      "confirmText": "This will message all {count} bound client(s).",
      "send": "Send"
    },
    "orders": {
      "refund": "Refund"
    },
    "refund": {
      "title": "Refund order #{id}?",
      "starsNote": "Telegram Stars will be refunded automatically.",
      "manualNote": "Marks the order refunded only — refund the money in the provider's dashboard.",
      "revoke": "Revoke granted days/traffic from this order",
      "done": "Refund processed"
    },
    "bot": {
      "enable": "Enable client bot",
      "pollTimeout": "Long-poll timeout (s)",
      "token": "Bot token (separate from admin bot)",
      "transportTitle": "Connection / transport",
      "transportHint": "How this bot reaches Telegram. Independent from the admin Telegram module.",
      "transport": "Transport",
      "outbound": "Outbound (sing-box) — requires core running",
      "noOutbounds": "No outbounds configured",
      "proxyUrl": "Proxy URL (http/https/socks5, empty = direct)",
      "proxyUser": "Proxy username (optional)",
      "proxyPass": "Proxy password (optional)"
    },
    "bindingDialog": {
      "addTitle": "Add binding",
      "editTitle": "Bind Telegram to {name}",
      "client": "Client",
      "tgId": "Telegram user ID (0 = unbind)"
    },
    "unbind": {
      "title": "Unbind Telegram from {name}?",
      "text": "This removes the Telegram link for client #{clientId} (Telegram ID {tgUserId}). The client stays in the panel; only the binding is cleared.",
      "confirm": "Unbind"
    },
    "transportModes": {
      "proxy": "Proxy",
      "outbound": "Outbound (sing-box)"
    }
  }
}
