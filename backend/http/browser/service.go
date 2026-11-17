// Package browser owns Panel browser sessions, cookie policy and CSRF admission.
package browser

// Settings is the existing read-only configuration consumed by browser security.
type Settings interface {
	GetSessionGeneration() (string, error)
	GetForceCookieSecure() (bool, error)
	GetWebURI() (string, error)
	GetWebDomain() (string, error)
	GetSessionSameSiteStrict() (bool, error)
}

type Service struct {
	Settings   Settings
	LogWarning func(...any)
}

func (b *Service) warning(args ...any) {
	if b.LogWarning != nil {
		b.LogWarning(args...)
	}
}
