export default {
  "telegram": {
    "title": "Telegram",
    "enabled": "Включено",
    "botToken": "Bot Token",
    "chatId": "Chat ID",
    "proxyUrl": "Proxy URL",
    "proxyUsername": "Proxy Username",
    "proxyPassword": "Proxy Password",
    "cpuThreshold": "Порог CPU",
    "notifyCpu": "Алерт CPU",
    "report": "Отчёт",
    "reportCron": "Cron отчёта",
    "securityWarning": "Telegram выключен по умолчанию. Proxy URL валидируется на сервере; токены и proxy-credentials хранятся как secret-поля.",
    "transport": "Транспорт",
    "transportProxy": "Прокси",
    "transportOutbound": "Outbound (sing-box)",
    "outboundLabel": "Outbound требует запущенного ядра",
    "noOutbounds": "Outbound не настроены",
    "hint": {
      "chatId": "Числовой Telegram chat/user ID для уведомлений. Его можно узнать через @userinfobot.",
      "cpuThreshold": "Отправлять предупреждение, когда CPU выше этого порога. По умолчанию: 90. Диапазон 1-100.",
      "reportCron": "Cron из 5 полей для отчёта, например 0 9 * * *. Пусто: выключено.",
      "transport": "Способ доступа бота к Telegram: proxy URL или outbound sing-box. По умолчанию: Proxy.",
      "backupMaxSize": "Пропустить бэкап, если зашифрованная БД больше этого размера. По умолчанию: 45 МБ. Диапазон 1-50."
    },
    "backup": {
      "title": "Резервная копия БД в Telegram",
      "enabled": "Бэкап в Telegram",
      "passphrase": "Парольная фраза Backup",
      "passphraseHint": "Этот пароль шифрует все бэкапы, отправляемые в Telegram, и опционально ручные бэкапы из Backup & Restore. Запомните его: без него файлы из Telegram нельзя восстановить ни через панель, ни локально. Восстановление через панель: загрузите файл в Backup & Restore и введите этот пароль. Локальная расшифровка: подкоманда основного бинарника s-ui decrypt-backup.",
      "passphraseMinLength": "Используйте не менее 12 символов.",
      "cron": "Cron бэкапа",
      "cronInvalid": "Используйте cron из 5 полей; шаг должен быть не меньше 1 минуты.",
      "schedule": {
        "title": "Периодичность backup",
        "manual": "Только вручную",
        "every15m": "Каждые 15 минут",
        "every30m": "Каждые 30 минут",
        "hourly": "Каждый час",
        "every6h": "Каждые 6 часов",
        "every12h": "Каждые 12 часов",
        "daily3": "Ежедневно в 03:00",
        "custom": "Свой интервал",
        "advanced": "Расширенный cron",
        "customValue": "Каждые",
        "customUnit": "Единица",
        "advancedCron": "Расширенный cron",
        "minutes": "минуты",
        "hours": "часы",
        "errors": {
          "customMinutesRange": "Укажите 1-59 минут.",
          "customHoursRange": "Укажите 1-23 часа.",
          "advancedCronInvalid": "Используйте cron из 5 полей; шаг должен быть не меньше 1 минуты."
        }
      },
      "excludeTables": "Исключаемые таблицы",
      "maxSize": "Максимальный размер",
      "sendNow": "Отправить сейчас",
      "tables": {
        "stats": "Статистика",
        "client_ips": "IP клиентов",
        "audit_events": "События аудита",
        "changes": "Изменения"
      }
    }
  }
}
