package componenthost

import (
	"net/http"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/service"
)

// OutboundHTTPClientProvider resolves an explicitly selected core outbound.
// The caller owns each returned client and closes its idle connections after
// the request. Request contexts still own cancellation; no default outbound or
// transport policy is selected by this port.
type OutboundHTTPClientProvider interface {
	CoreHTTPClient(tag string, timeout time.Duration) (*http.Client, error)
}

type OutboundHTTPClientFunc func(string, time.Duration) (*http.Client, error)

func (f OutboundHTTPClientFunc) CoreHTTPClient(tag string, timeout time.Duration) (*http.Client, error) {
	return f(tag, timeout)
}

// AuditRecorder preserves the existing audit owner's validation, redaction
// and delivery semantics without giving the consumer access to its Runtime.
type AuditRecorder interface {
	Record(service.AuditEvent) error
}
