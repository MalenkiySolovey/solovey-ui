# What's New in v1.5.7-beta7 — June 2026

# 🇬🇧 English

A maintenance release — **no new features**, just fixes, security hardening, and
small speed-ups from a fresh independent code review. **Nothing to migrate by hand.**

## 🔄 Changed

- **Scheduled 3x-ui sync & remote import removed.** Importing from 3x-ui is now a
  **one-time local `.db` upload** (UI wizard, API, and `import-xui --src`) —
  dry-run, conflict policy, plan/apply, and rollback all stay. If you relied on
  automatic remote sync, run your own download + one-shot import.

## 🐛 Fixes

- **Correct traffic charts.** Per-client usage graphs now total each time bucket
  instead of showing only the first sample.
- **No more sidebar flicker.** The navigation drawer no longer toggles itself on
  window resize/re-render.
- **More reliable upgrades.** The 1.3 data migration now runs inside a transaction
  and checks every write.
- **Clearer startup.** If the sing-box core fails to start, the panel says so
  plainly and **stays up** so you can fix the config from the UI.

## 🔒 Security & Privacy

- **Login no longer reveals valid usernames.** Sign-in now takes the same time
  whether or not a username exists, closing a timing side-channel used for
  account enumeration.
- **URL credentials masked in logs.** Any `user:pass@host` inside a logged URL is
  now hidden.
- **Secure session cookies by default.**
- **Non-Latin (IDN) panel domains supported** — e.g. a panel served on
  `панель.рф` now works.

## ⚡ Performance

- **Faster order history** in Paid Subscriptions (indexed lookups — no more full
  scans).
- **Lighter web panel** — dropped unused front-end dependencies for a smaller
  download.

---

# 🇷🇺 Русский

Сервисный релиз — **новых функций нет**, только исправления, усиление
безопасности и небольшие ускорения по итогам независимого код-ревью. **Ручная
миграция не нужна.**

## 🔄 Изменения

- **Убраны плановая синхронизация 3x-ui и удалённый импорт.** Импорт из 3x-ui
  теперь — **разовая локальная загрузка `.db`** (мастер в UI, API и
  `import-xui --src`); dry-run, политика конфликтов, plan/apply и откат
  сохранены. Если вы полагались на авто-синхронизацию — делайте свою загрузку +
  разовый импорт.

## 🐛 Исправления

- **Правильные графики трафика.** Графики расхода по клиенту теперь суммируют
  каждый интервал, а не показывают только первый отсчёт.
- **Боковая панель больше не мигает.** Drawer навигации не переключается сам при
  ресайзе/перерисовке.
- **Надёжнее обновления.** Миграция данных 1.3 теперь выполняется в транзакции с
  проверкой каждой записи.
- **Понятнее старт.** Если ядро sing-box не запустилось, панель прямо сообщает об
  этом и **остаётся доступной**, чтобы вы поправили конфиг через UI.

## 🔒 Безопасность и приватность

- **Логин больше не выдаёт существующие имена.** Вход теперь занимает одинаковое
  время независимо от того, есть ли такой пользователь — закрыт тайминг-канал
  перечисления учёток.
- **Маскировка учётных данных в URL в логах.** Любой `user:pass@host` внутри URL
  в логе теперь скрыт.
- **Secure-cookie сессии по умолчанию.**
- **Поддержка IDN-доменов панели** — например, панель на `панель.рф` теперь
  работает.

## ⚡ Производительность

- **Быстрее история заказов** в «Платных подписках» (индекс — без полного
  сканирования).
- **Легче веб-панель** — удалены неиспользуемые фронт-зависимости, меньше размер
  загрузки.

---

# 🇨🇳 简体中文

维护版本——**无新功能**，仅包含来自独立代码复审的修复、安全加固与小幅提速。**无需手动迁移。**

## 🔄 变更

- **移除 3x-ui 定时同步与远程导入。** 现在从 3x-ui 导入为**一次性本地 `.db` 上传**
  （UI 向导、API 及 `import-xui --src`）；dry-run、冲突策略、plan/apply 与回滚均保留。
  若你依赖自动远程同步，请自行下载后做一次性导入。

## 🐛 修复

- **正确的流量图表。** 每客户端用量图现在按时间桶求和，而不再只显示第一个样本。
- **侧边栏不再闪烁。** 导航抽屉不再在窗口缩放/重渲染时自行切换。
- **更可靠的升级。** 1.3 数据迁移现在在事务中运行并检查每次写入。
- **更清晰的启动。** 若 sing-box 内核启动失败，面板会明确提示并**保持可用**，以便你从 UI 修复配置。

## 🔒 安全与隐私

- **登录不再泄露有效用户名。** 无论用户名是否存在，登录耗时一致，关闭了用于账户枚举的时序侧信道。
- **日志中 URL 凭据被脱敏。** 日志 URL 中的 `user:pass@host` 现已隐藏。
- **会话 Cookie 默认 Secure。**
- **支持 IDN（非拉丁）面板域名**——例如 `панель.рф` 上的面板现在可用。

## ⚡ 性能

- **付费订阅的订单历史更快**（建立索引，不再全表扫描）。
- **更轻的 Web 面板**——移除未使用的前端依赖，下载更小。
