# Future Directions

Этот документ фиксирует не текущие правки, а план следующих этапов Solovey UI.
Цель: расширять панель без рассинхрона между UI, базой данных, generated
sing-box config и реальным поведением ядра.

## 1. Подписки из разных панелей и ядер

### Что уже есть

- Клиентские подписки уже собираются из локальных inbound/client links и могут
  отдавать sing-box JSON и Clash/Mihomo-подобный формат через `sub`.
- Remote Subscriptions уже умеют хранить внешние подписки, скачивать их,
  нормализовать подключения в sing-box outbound-JSON, объединять подключения в
  группы, синхронизировать группы в обычные outbounds и добавлять группы в
  клиентские links.
- Для внешних URL есть SSRF-защита через `util.GetExternalSub`, проверка URL и
  ограничения на приватные адреса.
- Для внешних подключений есть стабильная identity/source-key логика: при
  обновлении подписки стараемся обновлять существующее подключение, а не плодить
  дубликаты.

### Чего не хватает

- Единого subscription parser pipeline с явными стадиями:
  `fetch -> decode -> detect format -> parse links/config -> normalize -> validate -> diff`.
- Полного набора форматов входящих подписок: raw URI list, base64 URI list,
  sing-box JSON, Clash/Mihomo YAML, v2ray/xray JSON, fragment/noise/mux variants,
  Reality/uTLS/ech/fingerprint edge cases.
- Поддержки конфликтующих правил разных панелей: разные имена полей, разные
  defaults, разные трактовки `flow`, `host`, `sni`, `fp`, `alpn`, websocket path,
  grpc service name, reality short_id/public_key.
- Отдельного отчета импорта: сколько строк распознано, сколько пропущено, почему
  пропущено, какие поля были нормализованы или отброшены.

### Какие панели/ядра учитывать

- s-ui/admin8800/s-ui-x/2s-ui/S-UI-PRO: sing-box-ориентированные схемы и legacy
  базы.
- 3x-ui/x-ui: Xray/V2Ray share links, subscription page, client/inbound accounting.
- Marzban/Hiddify: richer subscription formats, Clash/Mihomo output, user-facing
  subscription pages.
- Mihomo/Clash clients: YAML proxy/provider groups, health-check URL, selector,
  url-test, fallback.
- sing-box: native JSON outbounds, route/dns/rule_set semantics, versioned
  deprecations.

### Риски

- Нельзя превращать unsupported field в silently ignored field. Лучше показать
  warning и сохранить raw metadata, чем создать "рабочий" outbound с другим
  поведением.
- URLTest/Selector ломаются, если ссылаться на исчезнувший outbound. При refresh
  внешней подписки нужна проверка referenced tags и понятный repair plan.
- Некоторые возможности ядра зависят от build tags (`with_utls`,
  `with_naive_outbound`). UI должен показывать причину, а не просто давать
  сохранить конфиг, который не стартует.

### Порядок внедрения

1. Вынести parser pipeline в отдельный backend-пакет с golden tests на реальные
   примеры подписок.
2. Сделать import report для Remote Subscriptions.
3. Добавить поддержку Clash/Mihomo YAML как входа.
4. Добавить поддержку v2ray/xray share links с round-trip тестами.
5. Добавить repair policy для исчезнувших/переименованных подключений.

## 2. Отдача подписки во всех форматах

### Что уже есть

- `sub.JsonService` формирует sing-box JSON для клиента.
- `sub.ClashService` конвертирует в Clash/Mihomo-подобный формат.
- `LinkService` добавляет внешние links и умеет подмешивать client info.
- Remote groups уже попадают в клиентские links как `remoteGroup`, а затем
  превращаются в outbounds подписки клиента.

### Чего не хватает

- Явной модели output formats: `sing-box`, `clash/mihomo`, `v2ray-uri-list`,
  `xray-json`, `raw-links`, возможно `stash/shadowrocket` profiles.
- Форматирование сейчас тесно связано с сервисами подписки; нужен слой
  `subscription/renderers`, который получает нормализованную модель и отдает
  конкретный формат.
- Нет UI-матрицы "какие форматы включены", "какой путь у формата", "какие
  defaults применяются", "добавлять ли direct/block/dns/rules".
- Нужны contract tests: один и тот же клиент + remote groups должны давать
  корректные результаты во всех включенных форматах.

### Как организовать генерацию

- Backend должен иметь внутреннюю normalized model:
  `Profile -> Outbounds -> Groups -> Rules -> Metadata`.
- Renderers должны быть чистыми функциями без обращения к БД.
- Сервис подписки должен только загружать client/inbounds/settings, собирать
  normalized profile и передавать его renderer.
- Clash/Mihomo renderer должен отдельно валидировать group references, потому что
  там порядок и provider groups могут менять реальное поведение.

### Что показывать в UI

- Включение/выключение форматов.
- Путь формата и полный preview URL.
- Compatibility presets: sing-box, Mihomo, Clash.Meta, v2rayN, Nekoray.
- Advanced: direct rules, private IP/geosite rules, mux/fragment/noise, udp/tfo
  defaults, health-check URL.

### Порядок внедрения

1. Вынести normalized profile и renderer interface.
2. Покрыть текущие JSON/Clash golden tests.
3. Добавить raw URI/base64 URI output.
4. Добавить Mihomo-specific provider/group output.
5. Сделать UI для форматов и preview.

## 3. Что полезного переносить из 3x-ui

### Быстро перенести

- Подписочная HTML-страница с шаблонами: полезно для пользователя клиента и не
  требует менять sing-box config.
- Более явные backup/restore сценарии в панели, включая проверку внешних tools.
- Расширенный install flow: SSL/acme setup, проверка зависимостей, понятные
  сообщения при rollback.
- API docs page или хотя бы встроенная страница с endpoints/token scopes.

### Средняя сложность

- Схемы как источник истины. В 3x-ui frontend движется к Zod + generated types.
  Для Solovey UI аналогом может стать единая schema для backend settings,
  frontend forms и validation.
- Чистые link/parser функции с golden fixtures. Это особенно полезно для Remote
  Subscriptions и клиентских links.
- Улучшенная система traffic/accounting, last online, IP limit reporting.
- Node/cluster abstractions. Нужно аккуратно: sing-box panel сейчас не должна
  внезапно становиться оркестратором, но структура пригодится для будущих
  multi-server сценариев.

### Сложно

- PostgreSQL backend. Полезно для больших инсталляций, но затрагивает backup,
  migrations, tests, install script и runtime config.
- Полный multi-node management. Потребует agent protocol, auth, transport,
  monitoring, distributed config apply/rollback.
- Глубокий Xray/V2Ray compatibility layer. Его надо делать только через
  нормализованную модель и тесты, иначе появится спагетти между ядрами.

### Опасно или сомнительно

- Копировать Xray-specific UI напрямую. У sing-box другая модель DNS/route/rules,
  поэтому прямой перенос ломает ожидания.
- Автоматически включать fail2ban/iptables без явного UI и rollback. Это может
  закрыть доступ к серверу.
- Тянуть крупный React/Zod frontend подход целиком. Лучше переносить принципы:
  schema-first validation, generated types, pure adapters.

### Roadmap переноса

1. Parser/link golden tests.
2. Subscription page templates.
3. Backup/restore UI improvements.
4. Schema-first settings/forms.
5. Optional Postgres research.
6. Multi-node только после стабилизации single-node panel.

## 4. Недостающие настройки sing-box и UI

### Уже покрыто панелью

- Основные inbounds/outbounds/endpoints/TLS.
- Route rules, DNS rules, rule sets, DNS servers.
- Клиенты, subscriptions, paid subscriptions, generated sing-box config preview.
- Diagnostics/logs вокруг запуска панели и ядра.

### Что нужно добавить или проверить

- Build capabilities: uTLS, naive outbound, gVisor, Tailscale, ACME. UI должен
  понимать, поддержано ли это текущим бинарником, и показывать warning до save.
- Outbound advanced: dial fields, detour, bind interface, domain strategy,
  tcp fast open, udp fragment, fallback delay, network strategy.
- DNS advanced: fakeip, independent cache, ECS/client subnet, ruleset behavior,
  final/first semantics, per-server strategy, disable cache, rewrite ttl.
- Route advanced: logical rules, sniffing, protocol/client matching,
  network/interface/address rules, rule_set match source/accept empty.
- Experimental: cache file, clash api, v2ray api, debug/listen controls.
- Observability: structured logs by category, core stderr/stdout parser,
  last failed generated config, last core restart reason.

### Что прятать в Advanced

- Поля, которые легко ломают конфиг: raw JSON injections, detour chains,
  endpoint-specific routing, low-level dial options, DNS cache/fakeip internals.
- Build-tag-dependent функции: naive/uTLS/gVisor/Tailscale/ACME.
- Любые настройки, которые требуют понимать порядок обработки sing-box rules.

### Валидация

- Все tag references должны проверяться до save: route outbound, DNS server,
  urltest/selector outbounds, rule sets, remote group outbounds.
- Для "first/final" семантики нужно показывать реальный порядок, а не UI-иллюзию.
- Generated config должен проходить parser check bundled sing-box до restart core.
- При ошибке save/import нужна diagnostics подсказка: какая секция, какой tag,
  какой build capability.

### Порядок внедрения

1. Capability detection и warning before save.
2. Tag reference validator для всех секций.
3. Advanced mode для DNS/route/outbound.
4. Renderer/preview для changed generated config.
5. Логи с привязкой к последнему save/import/restart.
