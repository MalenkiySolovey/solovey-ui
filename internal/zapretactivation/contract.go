// Package zapretactivation defines the runtime-only projection returned by
// the authenticated broker. It contains no serialized sing-box options.
package zapretactivation

const SocketRoot = "/run/solovey-ui/zapret"

type Request struct {
	OutboundRef      string `json:"outboundRef"`
	OutboundRevision string `json:"outboundRevision"`
	StrategyRef      string `json:"strategyRef"`
}

type Grant struct {
	Request
	StrategyRevision string `json:"strategyRevision"`
	Generation       string `json:"generation"`
}

func (g Grant) ProtectPath(want Request) (string, bool) {
	if g.Request != want || !ValidDigest(want.OutboundRevision) || !ValidDigest(g.StrategyRevision) || !ValidDigest(g.Generation) {
		return "", false
	}
	return SocketRoot + "/z-" + g.Generation, true
}

func ValidDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
