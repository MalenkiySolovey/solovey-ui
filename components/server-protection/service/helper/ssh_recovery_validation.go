package helper

import (
	canonical "github.com/MalenkiySolovey/solovey-component-server-protection/service/helpercontract"
	"time"
)

func ValidSSHRecoveryResult(request SSHRecoveryObserveRequest, result *SSHRecoveryResult, now time.Time) bool {
	return canonical.ValidSSHRecoveryResult(request, result, now)
}
