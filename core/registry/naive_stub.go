//go:build !with_naive_outbound

package registry

import (
	"github.com/sagernet/sing-box/adapter/outbound"
)

const SupportsNaiveOutbound = false

func registerNaiveOutbound(registry *outbound.Registry) {
	// The optional outbound is intentionally absent in this build. Reporting
	// that as a startup error would make a healthy configuration look broken.
}
