package server

import (
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"
)

const (
	SubEnumWindow    = 15 * time.Minute
	SubEnumThreshold = 10
	SubEnumMaxKeys   = 4096
)

var subEnumerationTracker = ratelimit.NewThresholdWindow[string](SubEnumWindow, SubEnumThreshold, SubEnumMaxKeys, SubEnumWindow)

// NoteSubNotFound records invalid subscription-id lookups per source IP and
// emits a throttled audit warning once a scanner crosses the threshold.
func NoteSubNotFound(ip string) {
	if ip == "" {
		return
	}
	decision := subEnumerationTracker.Add(ip)
	if decision.Triggered {
		if hook := SubEnumerationAuditHook; hook != nil {
			hook(ip, decision.Count, int(SubEnumWindow.Minutes()))
		}
	}
}
