//go:build shtorm_extended && with_quic

package registry

import (
	"github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/protocol/hysteria2"
)

const supportsHysteriaRealm = true

func registerHysteriaRealmService(r *service.Registry) { hysteria2.RegisterRealmService(r) }
