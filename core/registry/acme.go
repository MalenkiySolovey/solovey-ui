//go:build with_acme

package registry

import (
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/service/acme"
)

const supportsACME = true

func registerACMEProvider(registry *certificate.Registry) {
	acme.RegisterCertificateProvider(registry)
}
