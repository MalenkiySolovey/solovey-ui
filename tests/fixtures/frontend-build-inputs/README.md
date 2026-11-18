# Accepted immutable frontend build-input fixture

This generated projection is the accepted M4A input tuple from Product commit 2b0f1252777efcc6d60446ec489fa4b83231ece2. Product components/sources.lock.json remains the source authority. This fixture admits independent Frontend CI without checking out a Product tree. It contains only the already verified UI projection, not a second editable component implementation.

- Manifest schema: solovey.frontend-build-inputs/v1
- Caller-trusted manifest SHA256: e5d37046dbae72dd57a4a547fcfe199a13fcaf2f50027ee2fd8a2d96cabc2c6a
- Archive SHA256: 7ac8154fd1c5beca5dae8cb57b7c482f6a42ec8a77fa85364a8618f6d4940a0b
- Archive schema: solovey.frontend-file-archive/v1; kind inputs; gzip of canonical UTF-8 JSON with regular relative file names and base64 bytes.
- Canonical source-lock digest: 5958a16c5678a3504eff1e11ba948d6d2b026601ede8ec65e06eab4da18a2a58
- Eight contributions: fallback-html, import-xui, observability-extra, paid-subscriptions, panel-update-ui, remote-outbound-subscriptions, server-protection, telegram.

Consumers must verify an independently admitted archive hash before extracting, then verify the trusted manifest digest with the accepted validator. Do not infer trust by hashing the supplied payload. New inputs require a new digest-named file and review; never replace this fixture in place.
