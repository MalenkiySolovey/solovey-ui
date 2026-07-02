export default {
  "telegram": {
    "title": "Telegram",
    "enabled": "启用",
    "botToken": "Bot Token",
    "chatId": "Chat ID",
    "proxyUrl": "代理 URL",
    "proxyUsername": "代理用户名",
    "proxyPassword": "代理密码",
    "cpuThreshold": "CPU 阈值",
    "notifyCpu": "CPU 告警",
    "report": "报告",
    "reportCron": "报告 Cron",
    "securityWarning": "Telegram 默认关闭。代理 URL 会在服务端校验；Token 和代理凭据以 secret 字段存储。",
    "transport": "连接方式",
    "transportProxy": "代理",
    "transportOutbound": "sing-box 出站",
    "outboundLabel": "出站连接需要核心正在运行",
    "noOutbounds": "未配置出站",
    "hint": {
      "chatId": "接收通知的 Telegram 聊天或用户数字 ID，可通过 @userinfobot 查询。",
      "cpuThreshold": "CPU 持续高于此百分比时发送通知。默认：90。范围：1-100。",
      "reportCron": "定期报告的 5 字段 cron，例如 0 9 * * *。留空表示关闭。",
      "transport": "机器人访问 Telegram 的方式：代理 URL 或 sing-box 出站。默认：代理。",
      "backupMaxSize": "加密数据库超过此大小时跳过备份。默认：45 MB。范围：1-50。"
    },
    "backup": {
      "title": "数据库备份到 Telegram",
      "enabled": "Telegram 备份",
      "passphrase": "备份密码短语",
      "passphraseHint": "此密码短语会加密发送到 Telegram 的所有备份，也可加密 Backup & Restore 中的手动备份。请牢记它：没有它，Telegram 文件无法在面板或本地恢复。面板恢复：在 Backup & Restore 上传文件并输入此密码。 本地解密：使用主程序子命令 s-ui decrypt-backup。",
      "passphraseMinLength": "请至少输入 12 个字符。",
      "cron": "备份 Cron",
      "cronInvalid": "请使用 5 字段 cron 表达式；步长至少为 1 分钟。",
      "schedule": {
        "title": "备份频率",
        "manual": "仅手动",
        "every15m": "每 15 分钟",
        "every30m": "每 30 分钟",
        "hourly": "每小时",
        "every6h": "每 6 小时",
        "every12h": "每 12 小时",
        "daily3": "每天 03:00",
        "custom": "自定义",
        "advanced": "高级 Cron",
        "customValue": "每",
        "customUnit": "单位",
        "advancedCron": "高级 Cron",
        "minutes": "分钟",
        "hours": "小时",
        "errors": {
          "customMinutesRange": "请输入 1-59 分钟。",
          "customHoursRange": "请输入 1-23 小时。",
          "advancedCronInvalid": "请使用 5 字段 cron 表达式；步长至少为 1 分钟。"
        }
      },
      "excludeTables": "排除表",
      "maxSize": "最大大小",
      "sendNow": "立即发送",
      "tables": {
        "stats": "统计",
        "client_ips": "客户端 IP",
        "audit_events": "审计事件",
        "changes": "变更"
      }
    }
  }
}
