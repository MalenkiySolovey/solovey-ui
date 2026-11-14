package helper

import (
	canonical "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helpercontract"
	"time"
)

func ValidSSHRecoveryResult(request SSHRecoveryObserveRequest, result *SSHRecoveryResult, now time.Time) bool {
	return canonical.ValidSSHRecoveryResult(request, result, now)
}
