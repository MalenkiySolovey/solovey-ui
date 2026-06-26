export default {
  "paidSub": {
    "experimental": "实验性",
    "secretboxWarning": "在生产环境中，请设置 SUI_SECRETBOX_KEY 环境变量，以便使用保存在数据库之外的密钥加密支付令牌。",
    "tabs": {
      "bindings": "绑定", "autoreg": "自动注册", "tariffs": "套餐",
      "payments": "支付", "messages": "消息", "orders": "订单", "bot": "机器人"
    },
    "active": "启用",
    "disabled": "禁用",
    "cols": {
      "client": "客户端", "clientId": "客户端 ID", "description": "描述",
      "telegramId": "Telegram ID", "expiry": "到期时间", "status": "状态",
      "name": "名称", "price": "价格", "stars": "Stars", "addDays": "+天",
      "addTraffic": "+流量", "enabled": "已启用", "id": "ID",
      "clientName": "客户端名称", "provider": "支付提供商", "amount": "金额", "created": "创建时间"
    },
    "bindings": {
      "hint": "将面板客户端绑定到 Telegram 用户 ID。绑定后，客户端可在机器人中获取链接、二维码、统计信息并完成支付。",
      "add": "添加绑定",
      "empty": "未找到客户端。请在客户端页面创建客户端（或启用自动注册），然后在此绑定 Telegram ID。",
      "none": "没有绑定"
    },
    "autoreg": {
      "enable": "自动注册未知用户", "inbounds": "新客户端使用的入站",
      "trialDays": "试用天数", "trialVolume": "试用流量（GB，0 = 不限量）",
      "maxClients": "自动注册客户端上限", "rateLimit": "/start 频率限制（每分钟）"
    },
    "tariffs": {
      "add": "添加套餐", "none": "没有套餐", "new": "新套餐", "edit": "编辑套餐",
      "priceMajor": "价格（主货币单位）", "currency": "货币", "starsAmount": "Stars 数量（XTR）",
      "addDays": "+天", "addTrafficGB": "+流量（GB，0 = 不限量）", "sort": "排序", "enabledField": "启用"
    },
    "payments": {
      "currency": "默认货币（例如 RUB、USD）", "orderTtl": "待支付订单有效期（分钟）",
      "stars": "Telegram Stars (XTR)", "yookassa": "YooKassa", "yookassaToken": "YooKassa provider_token（BotFather）",
      "stripe": "Stripe", "stripeToken": "Stripe provider_token（BotFather）",
      "paymaster": "PayMaster", "paymasterToken": "PayMaster provider_token（BotFather）",
      "crypto": "CryptoBot", "cryptoToken": "CryptoBot API 令牌", "external": "外部支付链接",
      "externalTemplate": "外部 URL 模板（https://…，可使用 {'{'}orderId{'}'} {'{'}amount{'}'} {'{'}currency{'}'} {'{'}clientId{'}'}）"
    },
    "messages": {
      "greetingTitle": "/start 欢迎语", "greetingHint": "已绑定客户端打开机器人时显示。留空则使用默认欢迎语。",
      "greetingLabel": "自定义欢迎语", "broadcastTitle": "向所有客户端群发",
      "broadcastHint": "向所有已绑定的 Telegram 用户发送一次性通知（{count} 位接收者）。",
      "broadcastLabel": "通知内容", "sendAll": "发送给所有人", "result": "已发送：{sent} · 失败：{failed}",
      "confirmTitle": "发送通知？", "confirmText": "消息将发送给全部 {count} 个已绑定客户端。", "send": "发送"
    },
    "orders": { "refund": "退款" },
    "refund": {
      "title": "为订单 #{id} 退款？", "starsNote": "Telegram Stars 将自动退还。",
      "manualNote": "这里只会将订单标记为已退款；请在支付提供商后台完成退款。",
      "revoke": "撤销此订单授予的天数/流量", "done": "退款已处理"
    },
    "bot": {
      "enable": "启用客户端机器人", "pollTimeout": "长轮询超时（秒）",
      "token": "机器人令牌（独立于管理员机器人）", "transportTitle": "连接 / 传输",
      "transportHint": "此机器人连接 Telegram 的方式，独立于管理员 Telegram 模块。",
      "transport": "传输", "outbound": "出站（sing-box）— 需要核心正在运行",
      "noOutbounds": "尚未配置出站", "proxyUrl": "代理 URL（http/https/socks5，留空 = 直连）",
      "proxyUser": "代理用户名（可选）", "proxyPass": "代理密码（可选）"
    },
    "bindingDialog": {
      "addTitle": "添加绑定", "editTitle": "将 Telegram 绑定到 {name}",
      "client": "客户端", "tgId": "Telegram 用户 ID（0 = 解除绑定）"
    },
    "unbind": {
      "title": "解除 {name} 的 Telegram 绑定？",
      "text": "这会删除客户端 #{clientId} 的 Telegram 关联（Telegram ID {tgUserId}）。客户端仍保留在面板中，仅清除绑定。",
      "confirm": "解除绑定"
    },
    "transportModes": { "proxy": "代理", "outbound": "出站（sing-box）" }
  }
}
