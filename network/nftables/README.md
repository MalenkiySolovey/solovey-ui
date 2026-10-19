# Owned nft platform adapter

This package owns the existing fixed `inet solovey_protection` mechanics. It
validates the bounded candidate grammar, checks platform support, observes the
owned table, atomically replaces it, and verifies apply and checkpoint rollback.
It does not own protection policy, desired state, authentication or operation
locks. The existing typed privileged broker retains those admission checks.

The system executor is private. Its closed operations cannot select arbitrary
executables, arguments, tables or shell commands. The deployment-owned managed
root retains artifact path and authority checks. A familiar table name is not
ownership: valid managed grammar and the sealed expected identity are required.

Unit and fake-executor regressions run with `go test ./network/nftables`.
Linux integration tests require `SUI_NFT_HOST_TESTS=1` **inside a disposable
network namespace**; CI builds one test binary and runs it with `unshare --net`.
They prove real nft check/apply/observe/rollback, temporal membership, foreign
table preservation and the existing fw4 flush/reload compatibility contract.
They must never be enabled on normal developer or operator host networking.
