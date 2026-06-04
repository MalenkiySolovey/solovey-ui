# What's New in v1.5.7 (Beta) — June 2026

# 🇬🇧 English

The headline of the 1.5.7 line is a brand-new **Paid Subscriptions** module:
a self-service Telegram bot that lets your end users get their subscription,
check usage, and pay or renew on their own. It's **experimental and off by
default** — existing setups are completely unaffected until you switch it on.

## ✨ New Features

- **Paid Subscriptions — a client-facing Telegram bot.** Bind a Telegram user
  to a client, and from inside Telegram they can grab their subscription link,
  per-protocol share links (VLESS/VMess/…) and **QR codes**, and see live usage:
  data used vs. limit, days remaining, online status, and total traffic.

- **Self-service sign-up with a free trial.** New users who open the bot can be
  auto-registered with a configurable trial (days + optional traffic), guarded
  by a global cap and per-user rate limits.

- **Built-in payments across 6 providers.** Define tariffs (price, +days,
  +traffic) and let clients buy or renew right in the bot. Supported:
  **Telegram Stars, YooKassa, Stripe, CryptoBot, PayMaster, and external links**.
  Renewals apply automatically and safely — no double-charging on retries.

- **In-bot Payment menu with self-service refunds.** A tidy **Payment** menu
  offers *Buy / Renew*, *My purchases*, and *Request a refund*. Telegram Stars
  refunds happen automatically; other providers route a refund request to you.

- **Admin refund tool.** Refund any order from the *Orders* tab, with a per-refund
  switch to optionally claw back the days and traffic that order granted.

- **Broadcasts & a custom greeting.** Send a one-off announcement to every bound
  client (with a sent/failed report), and edit the message users see on /start.

- **Flexible Telegram routing.** Send bot traffic through a proxy
  (HTTP/HTTPS/SOCKS5) or through one of your own sing-box outbounds — configured
  independently for the client bot and your admin notifications.

## 🐛 Fixes (affects everyone)

- **No more accidental duplicates.** Saving a client, inbound, or outbound briefly
  restarts the core, and a fast double-click (or a slow connection retry) could
  create two identical records. Now the Save button locks while saving and the
  server rejects duplicate submissions — **one action always creates one record.**

## 🔒 Security & Privacy

- All bot and payment-provider tokens are **encrypted at rest** and masked in the
  UI. For production, set `SUI_SECRETBOX_KEY` to keep that key outside the
  database (the panel warns you if it's missing). The bot only ever acts on the
  client it's bound to, and sensitive payment identifiers are never sent to the
  browser or written to logs.

> ⚠️ Paid Subscriptions is a **beta** feature and **disabled by default**. There's
> no manual migration — try it on a non-critical instance first.

---

# 🇷🇺 Русский

Главное в линейке 1.5.7 — новый модуль **«Платные подписки»**: Telegram-бот
самообслуживания, через который ваши конечные пользователи сами получают
подписку, смотрят расход и оплачивают или продлевают её. Модуль
**экспериментальный и выключен по умолчанию** — пока вы его не включите,
существующие установки никак не затрагиваются.

## ✨ Новые возможности

- **«Платные подписки» — клиентский Telegram-бот.** Привяжите Telegram-пользователя
  к клиенту — и прямо в Telegram он сможет получить ссылку подписки, ссылки по
  каждому протоколу (VLESS/VMess/…) и **QR-коды**, а также видеть статистику:
  израсходовано/лимит, сколько дней осталось, статус онлайн и суммарный трафик.

- **Саморегистрация с бесплатным пробным периодом.** Новый пользователь, открывший
  бота, может быть зарегистрирован автоматически с настраиваемым триалом (дни +
  опционально трафик) — с глобальным лимитом и ограничением частоты на пользователя.

- **Встроенная оплата через 6 провайдеров.** Задайте тарифы (цена, +дни, +трафик),
  и клиенты будут покупать или продлевать прямо в боте. Поддерживаются:
  **Telegram Stars, YooKassa, Stripe, CryptoBot, PayMaster и внешние ссылки**.
  Продление применяется автоматически и безопасно — без двойного списания при повторах.

- **Меню «Оплата» в боте с самостоятельными возвратами.** Аккуратное меню
  **«Оплата»** содержит *Купить / Продлить*, *Мои покупки* и *Оформить возврат*.
  Возврат Telegram Stars выполняется автоматически; по другим провайдерам заявка
  на возврат уходит вам.

- **Инструмент возврата для админа.** Верните любой заказ во вкладке *Orders*, с
  переключателем на каждый возврат — отзывать ли выданные этим заказом дни и трафик.

- **Рассылки и своё приветствие.** Отправьте разовое объявление всем привязанным
  клиентам (с отчётом «доставлено/ошибки») и отредактируйте сообщение, которое
  пользователи видят по /start.

- **Гибкая маршрутизация Telegram.** Направляйте трафик бота через прокси
  (HTTP/HTTPS/SOCKS5) или через один из ваших sing-box-аутбаундов — настраивается
  независимо для клиентского бота и админ-уведомлений.

## 🐛 Исправления (касаются всех)

- **Больше никаких случайных дубликатов.** Сохранение клиента, инбаунда или
  аутбаунда кратко перезапускает ядро, и быстрый двойной клик (или повтор при
  медленном соединении) мог создать две одинаковые записи. Теперь кнопка
  «Сохранить» блокируется на время сохранения, а сервер отклоняет повторные
  отправки — **одно действие всегда создаёт одну запись.**

## 🔒 Безопасность и приватность

- Все токены бота и платёжных провайдеров **шифруются на диске** и маскируются в
  интерфейсе. Для продакшена задайте `SUI_SECRETBOX_KEY`, чтобы ключ хранился вне
  базы (панель предупреждает, если переменная не задана). Бот действует только в
  отношении привязанного клиента, а чувствительные платёжные идентификаторы
  никогда не отправляются в браузер и не пишутся в логи.

> ⚠️ «Платные подписки» — **бета** и **выключены по умолчанию**. Ручная миграция
> не нужна — сначала попробуйте на некритичном экземпляре.

---

# 🇨🇳 简体中文

1.5.7 线的核心是全新的**「付费订阅」**模块：一个自助式 Telegram 机器人，让你的
终端用户自行领取订阅、查看用量、自助购买或续费。该功能为**实验性且默认关闭**——
在你启用之前，现有部署完全不受影响。

## ✨ 新功能

- **「付费订阅」——面向客户的 Telegram 机器人。** 将 Telegram 用户绑定到客户端后，
  用户即可在 Telegram 内获取订阅链接、各协议（VLESS/VMess/…）的分享链接和 **二维码**，
  并查看实时用量：已用/上限、剩余天数、在线状态以及总流量。

- **带免费试用的自助注册。** 打开机器人的新用户可被自动注册，并获得可配置的试用期
  （天数 + 可选流量）；受全局上限和每用户频率限制保护。

- **内置 6 家支付渠道。** 定义套餐（价格、+天数、+流量），客户即可在机器人内购买或
  续费。支持：**Telegram Stars、YooKassa、Stripe、CryptoBot、PayMaster 和外部链接**。
  续费自动且安全地生效——重试时不会重复扣费。

- **机器人内「支付」菜单与自助退款。** 简洁的**「支付」**菜单提供*购买 / 续费*、
  *我的购买*和*申请退款*。Telegram Stars 自动退款；其他渠道则将退款申请转交给你。

- **管理员退款工具。** 在 *Orders* 标签页对任意订单退款，并带有逐笔开关，可选择是否
  撤销该订单发放的天数与流量。

- **群发与自定义问候语。** 向所有已绑定客户发送一次性公告（附「成功/失败」报告），
  并可编辑用户在 /start 时看到的消息。

- **灵活的 Telegram 路由。** 让机器人流量经由代理（HTTP/HTTPS/SOCKS5）或你自己的某个
  sing-box 出站转发——客户端机器人与管理员通知可分别独立配置。

## 🐛 修复（影响所有人）

- **不再意外重复创建。** 保存客户端、入站或出站时会短暂重启内核，快速双击（或慢速
  连接的重试）可能创建两条相同记录。现在保存按钮在保存期间会锁定，服务端也会拒绝
  重复提交——**一次操作始终只创建一条记录。**

## 🔒 安全与隐私

- 所有机器人与支付渠道令牌均**加密存储**并在界面中脱敏。生产环境请设置
  `SUI_SECRETBOX_KEY`，将密钥保存在数据库之外（未设置时面板会提示）。机器人只对
  其绑定的客户端执行操作，敏感的支付标识符绝不会发送到浏览器或写入日志。

> ⚠️「付费订阅」为 **Beta** 功能且**默认关闭**。无需手动迁移——请先在非关键实例上试用。
