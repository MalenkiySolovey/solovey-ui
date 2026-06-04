<!--
DRAFT / staging release notes — version not yet decided.
At release time: rename this file to docs/releases/v<version>.md, set the date,
and copy it to .github/RELEASE_NOTES_v<version>.md so the release workflow picks
up the body. Until then, this captures the unreleased changes so nothing is lost.
-->

# Release Notes: Unreleased

Release date: TBD

Improvements to the experimental **Paid Subscriptions** module: the client bot
gains a structured **Payment** section and self-service **refunds**, plus an
admin refund tool. The feature remains **off by default**; no core schema
migration.

## What changed

- **Bot menu: a "Payment" section.** The single "Buy / Renew" button is replaced
  by a **Payment** menu that opens a submenu with three items: **Buy / Renew**
  (the existing purchase flow), **My purchases**, and **Request a refund**. The
  **Stats** button is renamed to **My subscription** (icon 👤) — the subscription
  view itself is unchanged.
- **My purchases.** A read-only list of the requesting user's own orders (tariff,
  amount, status, date), scoped strictly by Telegram user id.
- **Refunds.** Telegram Stars are refunded automatically via the Bot API
  (`refundStarPayment`) when the user taps *Request a refund*. Every other
  provider (YooKassa, Stripe, PayMaster, CryptoBot, external link) instead sends
  the administrator a refund request, because the Bot API offers no fiat/crypto
  refund — the money is returned in the provider's own dashboard.
- **Admin refund tool.** The *Orders* tab gains a **Refund** action: for Stars it
  calls `refundStarPayment`; for other providers it marks the order `refunded`.
  A per-refund toggle controls whether the granted days/traffic are revoked.
- **Refund rollback policy.** A new setting `paidSubRefundRevoke` (default on)
  governs the bot's user-initiated Stars refund: on success it also rolls back
  the days and traffic that order granted — `expiry −= addDays` (floored at now),
  `volume −= addTrafficBytes` (floored at 0) — to prevent buy-use-refund abuse.
  The rollback is idempotent (a double refund is a no-op) and never disables the
  client. The user does not choose this; the admin sets the policy globally and
  picks per-refund in the panel.
- **Hardening.** The admin Orders API no longer serializes the Telegram charge id
  or the invoice idempotency key to the browser (mirroring how the raw provider
  payload is already hidden). A concurrent bot/panel refund that returns "already
  refunded" is treated as success rather than a failure, since Telegram Stars
  refunds are idempotent at the charge level.

## Upgrade

No manual migration; existing data is preserved and the feature stays **disabled
by default**. The order `status` column gains a new value `refunded`; no schema
change is required.

---

# Примечания к релизу: Unreleased

Дата релиза: уточняется

Улучшения экспериментального модуля **«Платные подписки»**: в клиентском боте
появляется структурированный раздел **«Оплата»** и **самостоятельные возвраты**,
плюс инструмент возврата для админа. Функция по-прежнему **выключена по
умолчанию**; миграции схемы ядра нет.

## Что изменилось

- **Меню бота: раздел «Оплата».** Единственная кнопка «Купить / Продлить»
  заменена разделом **«Оплата»**, открывающим подменю из трёх пунктов: **Купить /
  Продлить** (существующий путь покупки), **Мои покупки** и **Оформить возврат**.
  Кнопка **«Статистика»** переименована в **«Моя подписка»** (иконка 👤) — сам
  экран подписки не изменён.
- **Мои покупки.** Read-only список заказов *самого* пользователя (тариф, сумма,
  статус, дата), строго в рамках его Telegram id.
- **Возвраты.** Telegram Stars возвращаются автоматически через Bot API
  (`refundStarPayment`) по нажатию пользователя на *Оформить возврат*. Остальные
  провайдеры (YooKassa, Stripe, PayMaster, CryptoBot, внешняя ссылка) вместо
  этого отправляют админу заявку — в Bot API нет возврата для фиата/крипты, деньги
  возвращаются в кабинете провайдера.
- **Инструмент возврата у админа.** Во вкладке *Orders* появилось действие
  **«Возврат»**: для Stars оно вызывает `refundStarPayment`; для прочих помечает
  заказ `refunded`. Переключатель на каждый возврат определяет, отзывать ли
  выданные дни/трафик.
- **Политика отката возврата.** Новая настройка `paidSubRefundRevoke` (по
  умолчанию вкл.) управляет пользовательским авто-возвратом Stars из бота: при
  успехе он также откатывает выданные этим заказом дни и трафик — `expiry −=
  addDays` (не ниже now), `volume −= addTrafficBytes` (не ниже 0) — против абуза
  «купил → вернул → пользуюсь». Откат идемпотентен (повторный возврат — no-op) и
  не отключает клиента. Пользователь этот выбор не делает; админ задаёт политику
  глобально и выбирает per-refund в панели.
- **Усиление.** Админский Orders API больше не сериализует в браузер Telegram
  charge id и idempotency-ключ инвойса (как уже скрыт сырой provider payload).
  Параллельный возврат из бота/панели с ответом «already refunded» трактуется как
  успех, а не ошибка, т.к. возврат Telegram Stars идемпотентен на уровне charge.

## Обновление

Ручная миграция не нужна, данные сохраняются, функция **выключена по умолчанию**.
У колонки `status` заказа появляется новое значение `refunded`; изменение схемы
не требуется.
