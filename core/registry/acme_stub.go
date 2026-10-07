//go:build !with_acme

package registry

import (
	"context"
	"errors"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

const supportsACME = false

func registerACMEProvider(registry *certificate.Registry) {
	certificate.Register[option.ACMECertificateProviderOptions](registry, "acme", func(context.Context, log.ContextLogger, string, option.ACMECertificateProviderOptions) (adapter.CertificateProviderService, error) {
		return nil, errors.New("ACME certificate providers require with_acme")
	})
}
