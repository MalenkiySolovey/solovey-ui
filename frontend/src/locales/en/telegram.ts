export default {
  "telegram": {
    "title": "Telegram",
    "enabled": "Enabled",
    "botToken": "Bot Token",
    "chatId": "Chat ID",
    "proxyUrl": "Proxy URL",
    "proxyUsername": "Proxy Username",
    "proxyPassword": "Proxy Password",
    "cpuThreshold": "CPU Threshold",
    "notifyCpu": "CPU Alert",
    "report": "Report",
    "reportCron": "Report Cron",
    "securityWarning": "Telegram is disabled by default. Proxy URLs are validated server-side; tokens and proxy credentials are stored as secret fields.",
    "transport": "Transport",
    "transportProxy": "Proxy",
    "transportOutbound": "Outbound (sing-box)",
    "outboundLabel": "Outbound requires the core running",
    "noOutbounds": "No outbounds configured",
    "hint": {
      "chatId": "Numeric Telegram chat or user ID that receives alerts. Find it via @userinfobot.",
      "cpuThreshold": "Send an alert when CPU stays above this percentage. Default: 90. Range 1-100.",
      "reportCron": "5-field cron schedule for periodic reports, for example 0 9 * * *. Empty disables it.",
      "transport": "How the bot reaches Telegram: proxy URL or a sing-box outbound. Default: Proxy.",
      "backupMaxSize": "Skip backup when the encrypted database exceeds this size. Default: 45 MB. Range 1-50."
    },
    "backup": {
      "title": "Database backup to Telegram",
      "enabled": "Telegram backup",
      "passphrase": "Backup passphrase",
      "passphraseHint": "This passphrase encrypts all backups sent to Telegram and optional manual backups from Backup & Restore. Remember it: without it, Telegram files cannot be restored in the panel or locally. Panel restore: upload the file in Backup & Restore and enter this passphrase. Local decrypt: use the main binary subcommand s-ui decrypt-backup.",
      "passphraseMinLength": "Use at least 12 characters.",
      "cron": "Backup Cron",
      "cronInvalid": "Use a 5-field cron expression; steps must be at least 1 minute.",
      "schedule": {
        "title": "Backup frequency",
        "manual": "Manual only",
        "every15m": "Every 15 minutes",
        "every30m": "Every 30 minutes",
        "hourly": "Every hour",
        "every6h": "Every 6 hours",
        "every12h": "Every 12 hours",
        "daily3": "Daily at 03:00",
        "custom": "Custom",
        "advanced": "Advanced cron",
        "customValue": "Every",
        "customUnit": "Unit",
        "advancedCron": "Advanced cron",
        "minutes": "minutes",
        "hours": "hours",
        "errors": {
          "customMinutesRange": "Use 1-59 minutes.",
          "customHoursRange": "Use 1-23 hours.",
          "advancedCronInvalid": "Use a 5-field cron expression; steps must be at least 1 minute."
        }
      },
      "excludeTables": "Excluded tables",
      "maxSize": "Maximum size",
      "sendNow": "Send now",
      "tables": {
        "stats": "Stats",
        "client_ips": "Client IPs",
        "audit_events": "Audit events",
        "changes": "Changes"
      }
    }
  }
}
