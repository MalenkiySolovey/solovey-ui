package helper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	canonical "github.com/MalenkiySolovey/solovey-component-server-protection/service/helpercontract"
)

const ListenerOwnerObserverRevision = "pidfd-getfd-getsockname-so-cookie-v6only-process-evidence-systemd-procd-projection-v2"

type ListenerOwnerExecutor interface {
	Detect(context.Context) ListenerOwnerSupport
	Observe(context.Context, ListenerOwnerObserveRequest) (*ListenerOwnerObserveResult, error)
}

func sealListenerOwnerResult(result *ListenerOwnerObserveResult) {
	canonical.SealListenerOwnerResult(result)
}
func ListenerOwnerResultValid(result *ListenerOwnerObserveResult) bool {
	return canonical.ListenerOwnerResultValid(result)
}

func normalizedOwnerReasons(values []string) []string {
	return canonical.NormalizedOwnerReasons(values)
}
func listenerOwnerObserverDigest() string {
	sum := sha256.Sum256([]byte(ListenerOwnerObserverRevision))
	return hex.EncodeToString(sum[:])
}
