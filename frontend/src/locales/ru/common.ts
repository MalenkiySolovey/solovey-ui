export default {
  "nav": {
    "groups": {
      "proxy": "Прокси",
      "network": "Сеть",
      "integrations": "Интеграции",
      "system": "Система"
    }
  },
  "table": {
    "search": "Поиск",
    "rowsPerPage": "Строк на странице",
    "rowNumber": "#",
    "showingRange": "{from}–{to} из {total}",
    "selectAll": "Выбрать все",
    "selectRow": "Выбрать строку",
    "expandRow": "Показать детали строки",
    "clearFilters": "Сбросить фильтры",
    "noData": "Нет данных",
    "moveUp": "Переместить вверх",
    "moveDown": "Переместить вниз"
  },
  "form": {
    "unsavedChanges": "Несохранённые изменения",
    "leaveTitle": "Отменить изменения?",
    "leaveConfirm": "Есть несохранённые изменения. Отменить их?",
    "discard": "Отменить",
    "sections": {
      "basic": "Основное",
      "configuration": "Конфигурация"
    }
  },
  "success": "успех",
  "warning": "предупреждение",
  "failed": "ошибка",
  "enable": "Включить",
  "disable": "Отключить",
  "none": "Никакие",
  "all": "Все",
  "loading": "Загрузка...",
  "confirm": "Вы уверены?",
  "yes": "да",
  "no": "нет",
  "unlimited": "бесконечный",
  "type": "Тип",
  "protocol": "Протокол",
  "submit": "Отправить",
  "reset": "Сбросить",
  "now": "Сейчас",
  "network": "Сеть",
  "copyToClipboard": "Копировать в буфер обмена",
  "noData": "Нет данных!",
  "invalidLogin": "Неверный логин!",
  "online": "В сети",
  "status": "Статус",
  "version": "Версия",
  "email": "Электронная почта",
  "commaSeparated": "(разделено запятыми)",
  "count": "Количество",
  "template": "Шаблон",
  "editor": "Редактор",
  "error": {
    "dplData": "Дублирующие данные",
    "core": "Ошибка Sing-Box",
    "invalidData": "Неверные данные"
  },
  "theme": {
    "light": "Светлый",
    "dark": "Темный",
    "system": "Система"
  },
  "pages": {
    "login": "Вход",
    "home": "Дашборд",
    "inbounds": "Входящие",
    "outbounds": "Исходящие",
    "remoteOutboundSubscriptions": "Удалённые подписки",
    "services": "Сервисы",
    "endpoints": "Эндпоинты",
    "clients": "Клиенты",
    "rules": "Правила",
    "tls": "Настройки TLS",
    "basics": "Основы",
    "dns": "DNS",
    "admins": "Администраторы",
    "telegram": "Telegram",
    "audit": "Аудит",
    "diagnostics": "Диагностика",
    "migrateXui": "Миграция 3x-ui",
    "paidSub": "Платные подписки",
    "singBoxConfig": "Конфиг sing-box",
    "settings": "Настройки",
    "support": "Поддержать проект"
  },
  "support": {
    "title": "Поддержать Solovey UI",
    "intro": "Solovey UI является независимым проектом с открытым исходным кодом. Помочь можно качественным отчётом об ошибке, документацией, тестированием релизов или вкладом в код.",
    "noPaymentDetails": "У Solovey UI нет опубликованных официальных платёжных реквизитов. Здесь никогда не показываются чужие кошельки для пожертвований.",
    "github": "Открыть проект на GitHub",
    "issues": "Сообщить об ошибке",
    "imageAlt": "Логотип Solovey UI"
  },
  "main": {
    "tiles": "Плитки",
    "gauges": "Датчики",
    "charts": "Графики",
    "infos": "Информация",
    "gauge": {
      "cpu": "Загрузка ЦП",
      "mem": "Загрузка ОЗУ",
      "dsk": "Загрузка диска",
      "swp": "Загрузка Swap"
    },
    "chart": {
      "cpu": "Мониторинг ЦП",
      "mem": "Мониторинг ОЗУ",
      "net": "Сетевой трафик",
      "pnet": "Сетевые пакеты",
      "dio": "Мониторинг диска"
    },
    "info": {
      "sys": "Информация о системе",
      "sbd": "Информация о Sing-Box",
      "host": "Хост",
      "cpu": "ЦП",
      "core": "Ядро",
      "uptime": "Время работы",
      "startupTime": "Время запуска",
      "threads": "Потоки",
      "memory": "Память",
      "running": "Работает"
    },
    "backup": {
      "title": "Резервное копирование и восстановление",
      "backup": "Скачать резервную копию",
      "restore": "Восстановить резервную копию",
      "restoreHint": "Восстанавливает резервную копию s-ui (.db). Для импорта базы 3x-ui используйте «Миграция из 3x-ui» ниже, а не это.",
      "exclStats": "Исключить графики",
      "exclChanges": "Исключить изменения",
      "encryptTelegram": "Шифровать паролем Telegram backup",
      "encryptDisabledHint": "Задайте Backup passphrase во вкладке Telegram",
      "restorePassphrase": "Парольная фраза Backup",
      "sbConfig": "Скачать конфигурацию Sing-Box",
      "xui": {
        "title": "Миграция из 3x-ui",
        "hint": "Быстрый импорт применяется сразу. Полный мастер даёт предпросмотр, разбор конфликтов и выбор того, что именно переносить.",
        "button": "Быстрый импорт — выбрать .db 3x-ui…",
        "dryRun": "Сухой прогон (предпросмотр)",
        "strategy": "Стратегия конфликтов",
        "merge": "Объединить",
        "replace": "Заменить",
        "skip": "Пропустить",
        "summary": "Сводка импорта",
        "warnings": "Предупреждения",
        "openFull": "Полный мастер (просмотр и выбор)"
      }
    },
    "stats": {
      "title": "Использование и количество",
      "totalUsage": "Общее использование"
    }
  },
  "nexus": {
    "on": "Вкл",
    "off": "Выкл",
    "summary": {
      "inbounds": "Входящие: {total} • Онлайн: {online}",
      "outbounds": "Исходящие: {total} • Онлайн: {online}",
      "clients": "Клиенты: {total} • Онлайн: {online}",
      "endpoints": "Эндпоинты: {total} • Онлайн: {online}",
      "services": "Сервисы: {total}",
      "tls": "Сертификаты: {total} • ACME: {acme} • Reality: {reality}",
      "rules": "Наборы правил: {rulesets} • Правила: {rules}",
      "dns": "Серверы: {servers} • Правила: {rules}",
      "admins": "Администраторы: {total}"
    },
    "palette": {
      "label": "Палитра",
      "options": {
        "technical": "Технический",
        "navy": "Тёмно-синий",
          "emerald": "Emerald Minimal",
          "dracula": "Dracula Cyberpunk"
      }
    },
    "mode": {
      "label": "Режим интерфейса",
      "switchTo": "Переключить на режим {mode}",
      "options": {
        "classic": "Классический",
        "nexus": "Nexus"
      }
    },
    "status": {
      "online": "Онлайн",
      "offline": "Офлайн",
      "loading": "Загрузка",
      "failed": "Ошибка",
      "unavailable": "Недоступно",
      "running": "Запущен",
      "notRunning": "Не запущен",
      "statusMissing": "Статус отсутствует",
      "coreDown": "Ядро остановлено",
      "connected": "Подключено",
      "reconnecting": "Переподключение",
      "pollFallback": "Опрос",
      "realtime": "реальное время",
      "healthy": "Исправно",
      "degraded": "Частично",
      "down": "Недоступно",
      "historyReady": "История готова",
      "noHistory": "Нет истории",
      "idle": "Ожидание"
    },
    "overview": {
      "kpi": {
        "liveTraffic": "Текущий трафик",
        "liveTrafficDelta": "загрузка + отправка",
        "trafficStats": "Статистика трафика",
        "trafficStatsDelta": "Входящий {download} · Исходящий {upload}",
        "trafficTrend": "Тренд истории трафика",
        "onlineClients": "Клиенты онлайн",
        "clientSignal": "Текущий сигнал присутствия клиентов.",
        "enabledInbounds": "Включенные входящие",
        "activeInbounds": "{count} активны",
        "inboundOnlineTags": "{count} входящих тегов сообщили онлайн.",
        "health": "Состояние",
        "healthWaiting": "Ожидание данных статуса.",
        "healthHealthy": "Проверки статуса и sing-box онлайн.",
        "healthDown": "Проверка онлайн или sing-box недоступна.",
        "healthDegraded": "Один из сигналов состояния еще отсутствует."
      },
      "traffic": {
        "title": "Обзор трафика",
        "range24h": "История входящих за 24 ч",
        "loading": "Загрузка истории трафика.",
        "chartAria": "История входящей загрузки и отдачи",
        "emptyOffline": "История трафика недоступна, пока браузер офлайн.",
        "emptyUnavailable": "Не удалось загрузить историю трафика из текущей статистики входящих.",
        "emptyNoHistory": "Истории входящего трафика пока нет."
      },
      "system": {
        "title": "Состояние системы",
        "hostUptime": "Время работы хоста",
        "singboxUptime": "Время работы sing-box",
        "cpu": "CPU",
        "memory": "Память",
        "disk": "Диск",
        "realtime": "Реальное время",
        "noAddress": "Адрес не получен."
      },
      "clients": {
        "title": "Топ клиентов",
        "shown": "Показано: {count}",
        "loading": "Загрузка клиентов.",
        "empty": "Данных о трафике клиентов пока нет.",
        "state": "Состояние",
        "total": "Всего",
        "viewAll": "Все клиенты"
      },
      "events": {
        "title": "Последние события",
        "loading": "Загрузка событий аудита.",
        "rows": "Строк: {count}",
        "emptyOffline": "Последние события аудита недоступны офлайн.",
        "emptyUnavailable": "Не удалось загрузить последние события аудита.",
        "empty": "Последние события аудита не возвращены."
      },
      "protocols": {
        "title": "Сводка протоколов",
        "groups": "Групп протоколов: {count}",
        "type": "Тип",
        "activeShort": "Активно",
        "totalShort": "Всего",
        "tags": "Теги",
        "loading": "Загрузка входящих.",
        "empty": "Входящие не настроены.",
        "inboundTags": "Входящих тегов: {count}",
        "noTag": "Тег не получен."
      }
    }
  },
  "objects": {
    "inbound": "Входящий",
    "client": "Клиент",
    "outbound": "Исходящий",
    "endpoint": "Точка входа",
    "config": "Настройки",
    "rule": "Правило",
    "ruleset": "Набор правил",
    "service": "Сервис",
    "dnsserver": "DNS сервер",
    "dnsrule": "Правило DNS",
    "user": "Пользователь",
    "tag": "Тег",
    "listen": "Прослушивание",
    "dial": "Исходящее соединение",
    "tls": "TLS",
    "multiplex": "Мультиплекс",
    "transport": "Транспорт",
    "headers": "Заголовки",
    "key": "Ключ",
    "value": "Значение"
  },
  "actions": {
    "action": "Действие",
    "add": "Добавить",
    "addbulk": "Добавить пакетно",
    "editbulk": "Редактировать пакетно",
    "delbulk": "Удалить пакетно",
    "new": "Новый",
    "edit": "Редактировать",
    "del": "Удалить",
    "clone": "Клонировать",
    "test": "Тест",
    "testAll": "Тестировать все",
    "save": "Сохранить",
    "saveOrder": "Сохранить порядок",
    "cancelOrder": "Отменить порядок",
    "update": "Обновить",
    "sortByNameAsc": "Имя А-Я",
    "sortByNameDesc": "Имя Я-А",
    "submit": "Отправить",
    "set": "Установить",
    "generate": "Генерировать",
    "disable": "Отключить",
    "close": "Закрыть",
    "cancel": "Отмена",
    "refresh": "Обновить",
    "diagnose": "Диагностика",
    "restartApp": "Перезапустить приложение",
    "restartSb": "Перезапустить Singbox",
    "logoutAllAdmins": "Выйти всем администраторам"
  },
  "unsavedOrder": "Ручной порядок не сохранён. Всё равно уйти со страницы?",
  "presets": {
    "title": "RU/ZH пресеты маршрутизации и DNS",
    "subtitle": "Добавляет rule-set, маршрутизацию, DNS и cache-file в текущий несохранённый конфиг.",
    "preset": "Пресет",
    "proxyOutbound": "Proxy outbound",
    "directOutbound": "Direct outbound",
    "apply": "Применить к локальному конфигу",
    "preview": "Предпросмотр изменений",
    "sources": "Источники",
    "source": "Источник",
    "presetManaged": "Пресет",
    "custom": "Вручную",
    "selectOutbounds": "Перед применением выберите proxy и direct outbound.",
    "sameOutboundWarning": "Proxy и direct outbound совпадают. Разделение маршрутов не заработает, пока они не различаются.",
    "zhMainlandDirect": "ZH: Китай напрямую",
    "zhMainlandDirectDesc": "Маршрутизирует CN geosite/geoip напрямую, а не-CN домены через выбранный proxy outbound.",
    "zhNonCnProxy": "ZH: не-CN через proxy",
    "zhNonCnProxyDesc": "Добавляет разделение, где не-CN домены идут через proxy, а CN домены напрямую.",
    "ruBlockedProxy": "RU: заблокированные IP через proxy",
    "ruBlockedProxyDesc": "Маршрутизирует заблокированные диапазоны runetfreedom через proxy, приватные диапазоны напрямую."
  },
  "regionalPresets": {
    "title": "Региональные пресеты",
    "subtitle": "Настройте маршрутизацию и DNS для российских и китайских доменов в одном месте.",
    "open": "Региональные пресеты",
    "cancel": "Отмена",
    "preview": "Предпросмотр изменений",
    "apply": "Применить пресеты",
    "applied": "Региональные пресеты применены",
    "back": "Назад",
    "done": "Готово",
    "needFullControl": "Нужен полный контроль?",
    "editRulesManually": "Редактируйте правила вручную.",
    "proxyOutbound": "Proxy outbound",
    "directOutbound": "Direct outbound",
    "selectOutbounds": "Выберите proxy и direct outbound перед предпросмотром изменений.",
    "sameOutboundWarning": "Proxy и direct outbound совпадают. Разделение маршрутов не изменит трафик, пока они не различаются.",
    "region": {
      "ru": {
        "title": "RU маршрутизация и DNS",
        "description": "Готовая настройка для российских доменов и DNS-поведения."
      },
      "zh": {
        "title": "ZH маршрутизация и DNS",
        "description": "Готовая настройка для китайских доменов и DNS-поведения."
      },
      "status": {
        "notConfigured": "Не настроено",
        "enabled": "Включено",
        "pendingChange": "Включено, есть несохранённые изменения",
        "customDetected": "Найдены ручные изменения",
        "cannotApply": "Нельзя применить пресет"
      }
    },
    "direction": {
      "title": "Направление",
      "direct": {
        "title": "Напрямую",
        "description": "Региональные домены обходят proxy. Подходит для локальных сервисов, которые лучше работают из расположения сервера."
      },
      "proxy": {
        "title": "Через proxy",
        "description": "Региональные домены используют proxy. Подходит, если регион должен идти через выбранный proxy-маршрут."
      }
    },
    "dns": {
      "behavior": "DNS будет соответствовать режиму {mode} для доменов {region}."
    },
    "previewGroups": {
      "willAdd": "Будет добавлено",
      "willChange": "Будет изменено",
      "willKeep": "Будет сохранено",
      "willRemove": "Будет удалено",
      "noChanges": "Без изменений",
      "securityNote": "Ручные правила и DNS-записи будут сохранены. Изменения пресета применятся только после подтверждения.",
      "securityWarnings": "Предупреждения безопасности",
      "noWarnings": "Предупреждений нет"
    },
    "security": {
      "note": "Проверьте изменения перед применением. Пресеты могут влиять на приватность DNS и маршруты трафика.",
      "dnsLeakRisk": "Этот вариант может отправлять DNS-запросы для региональных доменов не тем путём, который соответствует выбранному маршруту.",
      "routeExposureRisk": "Этот вариант может отправлять региональный трафик не тем путём, который вы ожидаете.",
      "partialApplyBlocked": "Маршрутизация и DNS должны сохраняться вместе. Ничего не изменено.",
      "customItemsKept": "Ручные правила и DNS-записи будут сохранены."
    },
    "advanced": {
      "title": "Расширенные параметры",
      "exceptions": "Исключения",
      "exceptionsHelp": "Домены из списка не будут следовать этому региональному пресету.",
      "addDomain": "Добавить домен",
      "removeDomain": "Удалить домен",
      "noExceptions": "Исключения не добавлены.",
      "invalidDomain": "Введите корректный домен."
    },
    "result": {
      "customItemsKept": "Правила и DNS-записи пресета обновлены. Ручные элементы сохранены.",
      "failed": "Пресет не применён",
      "regionalDataUnavailable": "Необходимые региональные данные недоступны. Обновите региональные данные и повторите попытку."
    }
  },
  "delivery": {
    "title": "Выдача подписки",
    "rawLinks": "Сырые ссылки",
    "subscriptionUrl": "URL подписки",
    "importUrl": "URL импорта",
    "testUrl": "Проверить URL",
    "testOk": "URL подписки доступен. Это проверка доступности, а не валидности формата подписки.",
    "testFailed": "Проверка URL подписки не удалась.",
    "noRawLinks": "У этого клиента нет сохраненных сырых ссылок."
  },
  "login": {
    "title": "Вход",
    "username": "Имя пользователя",
    "unRules": "Имя пользователя не может быть пустым",
    "password": "Пароль",
    "pwRules": "Пароль не может быть пустым",
    "invalidCredentials": "Неверное имя пользователя или пароль."
  },
  "menu": {
    "logout": "Выйти",
    "language": "Язык",
    "theme": "Тема",
    "navigation": "Переключить навигацию"
  },
  "admin": {
    "addAdmin": "Добавить администратора",
    "deleteAdmin": "Удалить администратора",
    "username": "Имя пользователя",
    "changeCred": "Изменить учетные данные",
    "oldPass": "Текущий пароль",
    "newUname": "Новое имя пользователя",
    "newPass": "Новый пароль",
    "confirmPass": "Подтвердите пароль",
    "addValidation": "Заполните текущий пароль, имя пользователя и оба поля нового пароля.",
    "deleteValidation": "Текущий пароль обязателен.",
    "passwordMismatch": "Пароли не совпадают.",
    "deleteConfirm": "Этот администратор и его API-токены будут удалены. Продолжить?",
    "deleteSelfDisabled": "Нельзя удалить собственную учетную запись администратора.",
    "lastLogin": "Последний вход",
    "date": "Дата",
    "time": "Время",
    "changes": "Изменения",
    "logoutAll": "Выйти всем администраторам",
    "logoutAllConfirm": "Все web-сессии администраторов будут инвалидированы, включая вашу текущую сессию. API-токены не изменятся. Продолжить?",
    "actor": "Исполнитель",
    "key": "Ключ",
    "action": "Действие",
    "api": {
      "title": "Токены API",
      "msg": "Пожалуйста, скопируйте токен ниже и сохраните его в безопасном месте. Он не будет показан заново.",
      "token": "Токен",
      "scope": "Область доступа",
      "enabled": "Включён"
    }
  },
  "types": {
    "un": "Имя пользователя",
    "pw": "Пароль",
    "direct": {
      "overrideAddr": "Переопределить адрес",
      "overridePort": "Переопределить порт"
    },
    "hy": {
      "obfs": "Обфусцированный пароль",
      "auth": "Пароль аутентификации",
      "hyOptions": "Параметры Hysteria",
      "hy2Options": "Параметры Hysteria2",
      "ignoreBw": "Игнорировать пропускную способность клиента"
    },
    "shdwTls": {
      "hs": "Сервер рукопожатий",
      "addHS": "Добавить сервер рукопожатий"
    },
    "ssh": {
      "passphrase": "Парольная фраза",
      "hostKey": "Ключи хоста",
      "algorithm": "Алгоритмы ключей",
      "clientVer": "Версия клиента",
      "options": "Параметры SSH"
    },
    "tor": {
      "execPath": "Путь к исполняемому файлу",
      "dataDir": "Каталог данных",
      "extArgs": "Дополнительные аргументы"
    },
    "tuic": {
      "congControl": "Контроль перегрузок",
      "authTimeout": "Таймаут аутентификации",
      "hb": "Сердцебиение"
    },
    "tun": {
      "addr": "Адреса",
      "ifName": "Имя интерфейса",
      "excludeMptcp": "Исключить MPTCP",
      "fallbackRuleIndex": "Индекс правила iproute2 fallback",
      "resetMark": "Метка сброса Auto Redirect",
      "nfqueue": "NFQUEUE Auto Redirect"
    },
    "vless": {
      "flow": "Поток",
      "udpEnc": "Кодирование UDP пакетов"
    },
    "vmess": {
      "security": "Безопасность",
      "globalPadding": "Глобальное заполнение",
      "authLen": "Длина аутентификации"
    },
    "wg": {
      "privKey": "Приватный ключ",
      "pubKey": "Публичный ключ пира",
      "psk": "Предварительно разделенный ключ",
      "localIp": "Локальные IP",
      "worker": "Работники",
      "ifName": "Имя интерфейса",
      "sysIf": "Системный интерфейс",
      "options": "Параметры Wireguard",
      "allowedIp": "Разрешенные IP",
      "peer": "Пир",
      "peers": "Пиры"
    },
    "lb": {
      "defaultOut": "Исходящий по умолчанию",
      "interruptConn": "Прервать существующие соединения",
      "testUrl": "Тестовый URL",
      "interval": "Интервал",
      "tolerance": "Толерантность",
      "urlTestOptions": "Параметры URLTest"
    },
    "failover": {
      "title": "Группа отказоустойчивости",
      "members": "Приоритетные выходы",
      "primary": "Основной",
      "backup": "Резервный",
      "addMember": "Добавить выход",
      "probeTarget": "URL проверки",
      "interval": "Интервал проверки",
      "hysteresis": "Порог восстановления",
      "enabled": "Автоматическое переключение",
      "allDown": "Все выходы недоступны"
    },
    "ts": {
      "options": "Параметры Tailscale",
      "stateDir": "Каталог состояния",
      "authKey": "Ключ аутентификации",
      "relayServer": "Сервер ретрансляции",
      "relayServerPort": "Порт сервера ретрансляции",
      "relayEndpoints": "Статические точки ретрансляции",
      "systemInterface": "Системный интерфейс",
      "sysIfName": "Имя интерфейса",
      "sysIfMtu": "MTU интерфейса",
      "controlUrl": "URL управления",
      "ephemeral": "Эфемерный",
      "hostname": "Имя хоста",
      "acceptRoutes": "Принять маршруты",
      "exitNode": "Выходной узел",
      "allowLanAccess": "Разрешить доступ LAN",
      "advRoutes": "Рекламируемые маршруты",
      "advTags": "Рекламируемые теги",
      "advExitNode": "Рекламируемый выходной узел",
      "udpTimeout": "Таймаут UDP"
    },
    "oom": {
      "memoryLimit": "Лимит памяти",
      "safetyMargin": "Запас безопасности",
      "minInterval": "Минимальный интервал",
      "maxInterval": "Максимальный интервал",
      "checksBeforeLimit": "Проверок до лимита"
    },
    "derp": {
      "configPath": "Путь к конфигурации",
      "verifyClientEndpoint": "Проверить конечную точку клиента",
      "verifyClientUrl": "Проверить URL клиента",
      "meshWith": "Сеть с",
      "meshPsk": "Предварительно разделенный ключ",
      "meshPskFile": "Файл предварительно разделенного ключа",
      "stun": "Сервер STUN",
      "options": "Параметры DERP"
    },
    "naive": {
      "insecureConcurrency": "Небезопасная параллельность",
      "quic": "QUIC",
      "quicCongestion": "Управление перегрузкой QUIC",
      "udpOverTcp": "UDP через TCP",
      "streamReceiveWindow": "Окно приема потока",
      "quicSessionReceiveWindow": "Окно приема сессии QUIC"
    },
    "anytls": {
      "idleInterval": "Интервал проверки неактивных сессий",
      "idleTimeout": "Тайм-аут неактивной сессии",
      "minIdle": "Минимум неактивных сессий"
    }
  },
  "basic": {
    "log": {
      "title": "Журналы",
      "level": "Уровень",
      "output": "Вывод",
      "timestamp": "Включить метку времени"
    },
    "routing": {
      "title": "Маршрутизация",
      "defaultOut": "Исходящий по умолчанию",
      "defaultIf": "Сетевой интерфейс по умолчанию",
      "defaultRm": "Маршрут по умолчанию",
      "defaultDns": "DNS по умолчанию",
      "autoBind": "Автопривязка сетевого интерфейса"
    },
    "exp": {
      "storeFakeIp": "Хранить поддельный IP",
      "extController": "Внешний контроллер",
      "extUi": "Внешний интерфейс",
      "extUiDownloadUrl": "URL загрузки интерфейса",
      "extUiDownloadDetour": "Обход загрузки интерфейса",
      "secret": "Секрет",
      "defaultMode": "Режим по умолчанию",
      "allowOrigin": "Разрешить источник",
      "allowPrivate": "Разрешить частную сеть"
    },
    "hint": {
      "logOutput": "Файл лога ядра sing-box. Пусто: вывод в stderr. Пример: box.log.",
      "ntpServer": "NTP-сервер для синхронизации времени. По умолчанию: time.apple.com.",
      "ntpPort": "Порт NTP-сервера. По умолчанию: 123.",
      "ntpInterval": "Интервал синхронизации времени, в минутах. По умолчанию: 30.",
      "cachePath": "Файл кэша DNS, FakeIP и отклонённых доменов. По умолчанию: cache.db.",
      "cacheId": "Необязательное пространство имён для изоляции кэша профиля.",
      "debugListen": "Адрес отладочного Go pprof-сервера, например 127.0.0.1:8080.",
      "gcPercent": "Целевой процент сборщика мусора Go. Пусто: стандартное значение 100.",
      "memoryLimit": "Мягкий лимит памяти ядра, например 256MiB. Пусто: без лимита.",
      "maxStack": "Максимальный размер стека горутины в байтах. Пусто: стандартное значение Go.",
      "maxThreads": "Максимум потоков ОС для ядра. Пусто: стандартное значение Go.",
      "clashController": "Адрес Clash API, например 127.0.0.1:9090.",
      "clashSecret": "Bearer-токен для Clash API. Для внешнего доступа задайте надёжное значение.",
      "clashExtUi": "Локальный каталог дашборда Clash. Необязательно.",
      "clashExtUiUrl": "URL для загрузки дашборда Clash. Необязательно.",
      "clashDefaultMode": "Режим маршрутизации Clash по умолчанию. Необязательно.",
      "clashAllowOrigin": "Разрешённые CORS origins для Clash API, через запятую.",
      "v2rayListen": "Адрес V2Ray gRPC stats API, например 127.0.0.1:8080."
    }
  },
  "date": {
    "expiry": "Срок действия",
    "expired": "Истек",
    "d": "д",
    "h": "ч",
    "m": "м",
    "s": "с",
    "ms": "мс"
  }
}
