# Solovey UI 2026.2.1

Compared with `v2026.2.0`.

## English

- Reviewed the latest compatible external panel changes and ported only the parts that fit Solovey UI's architecture.
- Embedded Go timezone data in the binary and removed the runtime `tzdata` package from the Docker image.
- Added a protected TLS certificate pin probe for outbounds. The probe is CSRF-protected, requires write scope for API tokens, rejects private/loopback/link-local targets, and fills `certificate_public_key_sha256` from the outbound TLS editor.
- Hardened sing-box `experimental.clash_api.external_controller` and `experimental.v2ray_api.listen`: saved base configs now reject non-loopback control API listeners.
- Added a shared SSRF-safe HTTP client and moved component catalog, component bundle downloads, and managed rule-set downloads onto URL validation plus dial-time private-address blocking.
- Extended API route, CSRF, authorization, TLS pin, and sing-box config tests for the new security behavior.
- Preserved existing stronger local implementations for bcrypt password migration, `pinSHA256`, VLESS flow handling, and subscription group conversion instead of replacing them with weaker external patterns.

## Русский

- Просмотрены последние совместимые изменения внешних панелей; перенесены только те части, которые подходят архитектуре Solovey UI.
- Данные часовых поясов встроены в Go-бинарь, а Docker-образ больше не устанавливает отдельный пакет `tzdata`.
- Добавлен защищённый probe для получения TLS certificate pin у outbound-подключений. Запрос защищён CSRF, требует `write` scope у API-токена, отклоняет private/loopback/link-local цели и заполняет `certificate_public_key_sha256` из TLS-редактора outbound.
- Усилена проверка sing-box `experimental.clash_api.external_controller` и `experimental.v2ray_api.listen`: base config больше нельзя сохранить, если control API слушает не loopback.
- Добавлен общий SSRF-safe HTTP client; загрузка component catalog, component bundle и managed rule-set теперь проходит URL validation и dial-time блокировку private-адресов.
- Расширены тесты API route, CSRF, authorization, TLS pin и sing-box config под новое security-поведение.
- Сохранены более сильные локальные реализации для bcrypt-миграции паролей, `pinSHA256`, VLESS flow и конвертации групп подписок; более слабые внешние паттерны не переносились.
