//go:build !shtorm_extended || !with_quic

package registry

import "github.com/sagernet/sing-box/adapter/service"

const supportsHysteriaRealm = false

func registerHysteriaRealmService(*service.Registry) {}
