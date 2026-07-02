export default {
  "telegram": {
    "title": "Telegram",
    "enabled": "啟用",
    "botToken": "Bot Token",
    "chatId": "Chat ID",
    "proxyUrl": "代理 URL",
    "proxyUsername": "代理使用者名稱",
    "proxyPassword": "代理密碼",
    "cpuThreshold": "CPU 閾值",
    "notifyCpu": "CPU 告警",
    "report": "報告",
    "reportCron": "報告 Cron",
    "securityWarning": "Telegram 預設關閉。代理 URL 會在服務端驗證；Token 和代理憑證以 secret 欄位儲存。",
    "transport": "連線方式",
    "transportProxy": "代理",
    "transportOutbound": "sing-box 出站",
    "outboundLabel": "出站連線需要核心正在執行",
    "noOutbounds": "尚未設定出站",
    "hint": {
      "chatId": "接收通知的 Telegram 聊天或使用者數字 ID，可透過 @userinfobot 查詢。",
      "cpuThreshold": "CPU 持續高於此百分比時傳送通知。預設：90。範圍：1-100。",
      "reportCron": "定期報告的 5 欄位 cron，例如 0 9 * * *。留空表示關閉。",
      "transport": "機器人存取 Telegram 的方式：代理 URL 或 sing-box 出站。預設：代理。",
      "backupMaxSize": "加密資料庫超過此大小時略過備份。預設：45 MB。範圍：1-50。"
    },
    "backup": {
      "title": "資料庫備份到 Telegram",
      "enabled": "Telegram 備份",
      "passphrase": "備份密語",
      "passphraseHint": "此密語會加密傳送到 Telegram 的所有備份，也可加密 Backup & Restore 中的手動備份。請記住它：沒有它，Telegram 檔案無法在面板或本機恢復。面板恢復：在 Backup & Restore 上傳檔案並輸入此密語。本機解密：使用主程式子命令 s-ui decrypt-backup。",
      "passphraseMinLength": "請至少輸入 12 個字元。",
      "cron": "備份 Cron",
      "cronInvalid": "請使用 5 欄位 cron 表達式；步長至少為 1 分鐘。",
      "schedule": {
        "title": "備份頻率",
        "manual": "僅手動",
        "every15m": "每 15 分鐘",
        "every30m": "每 30 分鐘",
        "hourly": "每小時",
        "every6h": "每 6 小時",
        "every12h": "每 12 小時",
        "daily3": "每天 03:00",
        "custom": "自訂",
        "advanced": "進階 Cron",
        "customValue": "每",
        "customUnit": "單位",
        "advancedCron": "進階 Cron",
        "minutes": "分鐘",
        "hours": "小時",
        "errors": {
          "customMinutesRange": "請輸入 1-59 分鐘。",
          "customHoursRange": "請輸入 1-23 小時。",
          "advancedCronInvalid": "請使用 5 欄位 cron 表達式；步長至少為 1 分鐘。"
        }
      },
      "excludeTables": "排除資料表",
      "maxSize": "最大大小",
      "sendNow": "立即傳送",
      "tables": {
        "stats": "統計",
        "client_ips": "用戶端 IP",
        "audit_events": "稽核事件",
        "changes": "變更"
      }
    }
  }
}
