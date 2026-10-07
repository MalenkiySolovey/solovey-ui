package registry

import (
	"context"
	"errors"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/certificate"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

// CertificateProviderRegistry creates a fresh schema/constructor owner for each
// runtime or isolated validation. Product eligibility is declared here, beside
// registration, rather than inferred from the upstream aggregate include set.
func CertificateProviderRegistry() *certificate.Registry {
	registry := certificate.NewRegistry()
	for _, entry := range certificateDeclarations() {
		entry.register(registry)
	}
	return registry
}

func certificateDeclarations() []declaration[*certificate.Registry] {
	return []declaration[*certificate.Registry]{
		{typeName: "acme", buildTag: "with_acme", compiled: supportsACME, register: registerACMEProvider},
		{typeName: "tailscale", buildTag: tailscaleBuildTag, compiled: supportsTailscale, register: registerTailscaleProvider},
		{typeName: C.TypeCloudflareOriginCA, productUnavailable: "PRODUCT_FEATURE_NOT_ENABLED", register: registerOriginCAProvider},
	}
}

// Origin CA requires the TLS owner's provider, token and file eligibility work.
// Its schema is known, but the runtime cannot activate it before that boundary.
func registerOriginCAProvider(registry *certificate.Registry) {
	certificate.Register[option.CloudflareOriginCACertificateProviderOptions](registry, C.TypeCloudflareOriginCA, func(context.Context, log.ContextLogger, string, option.CloudflareOriginCACertificateProviderOptions) (adapter.CertificateProviderService, error) {
		return nil, errors.New("certificate providers for Origin CA are not enabled by the product")
	})
}
