package endpoints

import (
	"bytes"
	"encoding/json"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// PeerDeliveryPolicy belongs to the endpoint's Panel metadata. It never enters
// the core options and cannot change the serving peer's route authority.
type PeerDeliveryPolicy struct {
	PublicEndpoint   string          `json:"publicEndpoint"`
	DNS              json.RawMessage `json:"dns"`
	ClientAllowedIPs []string        `json:"clientAllowedIPs"`
	ClientKeepalive  *uint16         `json:"clientKeepalive"`
}

func peerDeliveryPolicy(extension []byte) (PeerDeliveryPolicy, string, error) {
	var policy PeerDeliveryPolicy
	if len(extension) > 1<<20 || len(extension) > 0 && json.Unmarshal(extension, &policy) != nil {
		return policy, "", ErrPeerConfigUnavailable
	}
	if policy.PublicEndpoint != "" {
		host, port, err := net.SplitHostPort(policy.PublicEndpoint)
		number, portErr := strconv.ParseUint(port, 10, 16)
		if err != nil || portErr != nil || number == 0 || !peerConfigHost(host) {
			return policy, "", ErrPeerConfigUnavailable
		}
	}
	if len(policy.ClientAllowedIPs) > 128 || policy.ClientAllowedIPs != nil && len(policy.ClientAllowedIPs) == 0 {
		return policy, "", ErrPeerConfigUnavailable
	}
	for _, prefix := range policy.ClientAllowedIPs {
		if _, err := netip.ParsePrefix(prefix); err != nil {
			return policy, "", ErrPeerConfigUnavailable
		}
	}
	if policy.ClientKeepalive != nil && *policy.ClientKeepalive > 3600 {
		return policy, "", ErrPeerConfigUnavailable
	}
	// Keep the legacy comma-separated representation and accept the native
	// managed-endpoint list without rewriting either representation on read.
	dns := "1.1.1.1, 9.9.9.9"
	if len(policy.DNS) > 0 && !bytes.Equal(bytes.TrimSpace(policy.DNS), []byte("null")) {
		var text string
		if json.Unmarshal(policy.DNS, &text) == nil {
			if text != "" {
				dns = text
			}
		} else {
			var list []string
			if json.Unmarshal(policy.DNS, &list) != nil || len(list) > 32 {
				return policy, "", ErrPeerConfigUnavailable
			}
			for _, value := range list {
				if !peerConfigHost(value) {
					return policy, "", ErrPeerConfigUnavailable
				}
			}
			dns = strings.Join(list, ", ")
		}
	}
	if !peerConfigLine(dns) {
		return policy, "", ErrPeerConfigUnavailable
	}
	if dns != "" {
		for _, value := range strings.Split(dns, ",") {
			if !peerConfigHost(strings.TrimSpace(value)) {
				return policy, "", ErrPeerConfigUnavailable
			}
		}
	}
	return policy, dns, nil
}
