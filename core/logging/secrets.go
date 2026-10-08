package logging

import (
	"context"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

type secretContextKey struct{}

// WithSecrets extends the existing final log-sink guard for generation-owned
// ephemeral credentials, including an accidental bare-value upstream message.
func WithSecrets(ctx context.Context, secrets []string) context.Context {
	return context.WithValue(ctx, secretContextKey{}, append([]string(nil), secrets...))
}

func (f *defaultFactory) sanitize(message string) string {
	for _, secret := range f.secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, redact.Marker)
		}
	}
	return message
}
