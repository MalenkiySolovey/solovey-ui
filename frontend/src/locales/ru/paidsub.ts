export default {
  "paidSub": {
    "experimental": "экспериментально",
    "secretboxWarning": "Для продакшена задайте переменную окружения SUI_SECRETBOX_KEY, чтобы платёжные токены шифровались ключом, хранящимся вне базы данных.",
    "tabs": {
      "bindings": "Привязки",
      "autoreg": "Авто-регистрация",
      "tariffs": "Тарифы",
      "payments": "Платежи",
      "messages": "Сообщения",
      "orders": "Заказы",
      "bot": "Бот"
    },
    "active": "активен",
    "disabled": "отключён",
    "cols": {
      "client": "Клиент",
      "clientId": "ID клиента",
      "description": "Описание",
      "telegramId": "Telegram ID",
      "expiry": "Истекает",
      "status": "Статус",
      "name": "Название",
      "price": "Цена",
      "stars": "Stars",
      "addDays": "+Дней",
      "addTraffic": "+Трафик",
      "enabled": "Включён",
      "id": "ID",
      "clientName": "Имя клиента",
      "provider": "Провайдер",
      "amount": "Сумма",
      "created": "Создан"
    },
    "bindings": {
      "hint": "Привяжите клиента панели к Telegram ID пользователя. После этого клиент получает ссылки/QR/статистику и может оплачивать в боте.",
      "add": "Добавить привязку",
      "empty": "Клиенты не найдены. Создайте клиента на странице Clients (или включите авто-регистрацию), затем привяжите его к Telegram ID здесь.",
      "none": "Нет привязок"
    },
    "autoreg": {
      "enable": "Авто-регистрация неизвестных пользователей",
      "inbounds": "Inbounds для новых клиентов",
      "trialDays": "Пробных дней",
      "trialVolume": "Пробный трафик (ГБ, 0 = безлимит)",
      "maxClients": "Макс. авто-зарегистрированных клиентов",
      "rateLimit": "Лимит /start (в минуту)"
    },
    "tariffs": {
      "add": "Добавить тариф",
      "none": "Нет тарифов",
      "new": "Новый тариф",
      "edit": "Редактировать тариф",
      "priceMajor": "Цена (в основных единицах)",
      "currency": "Валюта",
      "starsAmount": "Кол-во Stars (XTR)",
      "addDays": "+Дней",
      "addTrafficGB": "+Трафик (ГБ, 0 = безлимит)",
      "sort": "Сортировка",
      "enabledField": "Включён"
    },
    "payments": {
      "currency": "Валюта по умолчанию (напр. RUB, USD)",
      "orderTtl": "TTL ожидающего заказа (мин)",
      "stars": "Telegram Stars (XTR)",
      "yookassa": "YooKassa",
      "yookassaToken": "YooKassa provider_token (BotFather)",
      "stripe": "Stripe",
      "stripeToken": "Stripe provider_token (BotFather)",
      "paymaster": "PayMaster",
      "paymasterToken": "PayMaster provider_token (BotFather)",
      "crypto": "CryptoBot",
      "cryptoToken": "CryptoBot API-токен",
      "external": "Внешняя ссылка на оплату",
      "externalTemplate": "Шаблон внешнего URL (https://… с {'{'}orderId{'}'} {'{'}amount{'}'} {'{'}currency{'}'} {'{'}clientId{'}'})"
    },
    "messages": {
      "greetingTitle": "Приветствие на /start",
      "greetingHint": "Показывается привязанному клиенту при открытии бота. Оставьте пустым для приветствия по умолчанию.",
      "greetingLabel": "Своё приветствие",
      "broadcastTitle": "Рассылка всем клиентам",
      "broadcastHint": "Отправляет разовое объявление каждому привязанному пользователю Telegram ({count} получателей).",
      "broadcastLabel": "Текст объявления",
      "sendAll": "Отправить всем",
      "result": "Отправлено: {sent} · ошибок: {failed}",
      "confirmTitle": "Отправить объявление?",
      "confirmText": "Сообщение получат все {count} привязанных клиентов.",
      "send": "Отправить"
    },
    "orders": {
      "refund": "Возврат"
    },
    "refund": {
      "title": "Вернуть заказ #{id}?",
      "starsNote": "Telegram Stars будут возвращены автоматически.",
      "manualNote": "Только помечает заказ возвращённым — верните деньги в панели провайдера.",
      "revoke": "Отозвать выданные дни/трафик по этому заказу",
      "done": "Возврат выполнен"
    },
    "bot": {
      "enable": "Включить клиентский бот",
      "pollTimeout": "Таймаут long-poll (с)",
      "token": "Токен бота (отдельный от админ-бота)",
      "transportTitle": "Соединение / транспорт",
      "transportHint": "Как этот бот достигает Telegram. Независимо от админского модуля Telegram.",
      "transport": "Транспорт",
      "outbound": "Outbound (sing-box) — требует запущенного ядра",
      "noOutbounds": "Outbound не настроены",
      "proxyUrl": "Proxy URL (http/https/socks5, пусто = напрямую)",
      "proxyUser": "Имя пользователя прокси (необязательно)",
      "proxyPass": "Пароль прокси (необязательно)"
    },
    "bindingDialog": {
      "addTitle": "Добавить привязку",
      "editTitle": "Привязать Telegram к {name}",
      "client": "Клиент",
      "tgId": "Telegram ID пользователя (0 = отвязать)"
    },
    "unbind": {
      "title": "Отвязать Telegram от {name}?",
      "text": "Удаляет связь Telegram для клиента #{clientId} (Telegram ID {tgUserId}). Клиент остаётся в панели; очищается только привязка.",
      "confirm": "Отвязать"
    },
    "transportModes": {
      "proxy": "Proxy",
      "outbound": "Outbound (sing-box)"
    }
  }
}
