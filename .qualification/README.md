# Temporary Stage2 Wave5 executor

This branch is a Goal-owned test executor. Main's CI and production source stay
unchanged. Product source is merged main d14e14ab1ddcd126fc71a00197d249e45b42d05d;
reports record the separate fixture commit and actual binary/image hashes.
Windows uses the unchanged canonical reusable package producer. Docker uses the
exact main's Dockerfile and existing cache scopes, with local load and no push.

Manual dispatch of the registered ci.yml on this branch executes native Windows
amd64/arm64 and Docker arm64. Docker amd64 reuses the local warm builder. No
release, tag, registry publication, privilege/security change, emulator or router
mutation. Only existing public trust roots and fresh synthetic state are used.
This host proof does not replace board/systemd/OpenWrt/FriendlyWrt or remaining
old-state/client requirements.

The Go fixture adapts Solovey clients from core/runtime/quic_parents_test.go. Its
text suffix excludes ordinary package discovery. A runner copies it to an owned
temporary file and compiles against exact candidate source. It seeds only a
marked missing DB, uses normal bearer authorization and original QUIC verifiers,
then checks parent identity, remote close, shared-IP kick/reconnect and generation
fences. Product code executes separately from the fixture. An unused declared
Naive outbound exercises actual native engine initialization without external
traffic or disabling TLS verification; carrier interoperability remains separate.

No credential, raw auth response, private DB, bootstrap credential file or key is
retained as an artifact. Successful Goal-owned fixtures are removed after results.
