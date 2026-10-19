# Secret references over settings storage

SecretRef is a reference/access contract, not storage or plaintext. The first
consumer is Telegram's existing `telegramBotToken` encrypted setting. Identity is
the registered component owner plus its canonical setting key. Trusted host
composition supplies the component actor; it is never accepted from HTTP input.
In-process components remain trusted code, not sandboxed principals.

The first policy permits only PRESENCE and USE_FOR_OWNER_OPERATION with MASKED
projection. The service adapter verifies exact owner registration, encrypted-key
ownership, actor, operation and registration generation. Non-owner, ordinary
setting, unknown operation and raw reveal requests fail closed. No new endpoint
or privilege boundary is introduced; existing route scope/authentication remains
required before owner operations. Token reading now inherits the operation context.

Use captures one current stored row, decrypts through the existing Codec with
the canonical setting key as AAD, records a synchronous redacted admission audit,
then lends bytes to the owner callback. Borrowed bytes are wiped even on failure;
owner Bot API clients retain their necessary operation-local string copy. Callback,
database and codec error text is not copied into common output. Audit failure
prevents plaintext use. Secret use does not emit Health/Job/Change/journal payloads.

Metadata reports presence and an opaque ciphertext revision. Legacy plaintext
has no rotation revision; no plaintext hash or invented rotation time is exposed.
An admitted use may finish with its captured value during replacement; subsequent
use sees the replacement and its different revision. No mixed row/revision exists.
This does not revoke credentials already held by an active owner operation.

Replace/rotate/delete remain existing authenticated settings and component-owner
operations. This initial read policy grants none of them. Existing environment
and DB-master fallback, old storage, UI presence markers, backup, restore and
reseal paths remain authoritative. No new backend, key management, raw reveal,
secret history, or schema migration is added. Other secrets are not migrated.

Proof includes real Telegram stored-token discovery behind route authorization,
masked settings, replacement, database export/restore with environment-key reseal,
owner DropData, wrong key/AAD, owner isolation, canceled use, audit failure,
redacted audit correlation and deterministic concurrent replacement snapshots.
