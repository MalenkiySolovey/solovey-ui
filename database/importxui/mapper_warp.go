package importxui

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/deposist/s-ui-x/database/model"
)

// xrayOutbound is a single entry of the source Xray config's outbounds array.
type xrayOutbound struct {
	Tag      string          `json:"tag"`
	Protocol string          `json:"protocol"`
	Settings json.RawMessage `json:"settings"`
}

// xrayWireguardOutbound is the settings block of an Xray wireguard outbound
// (this is how 3x-ui stores Cloudflare WARP).
type xrayWireguardOutbound struct {
	MTU            int                         `json:"mtu"`
	SecretKey      string                      `json:"secretKey"`
	Address        []string                    `json:"address"`
	Workers        int                         `json:"workers"`
	DomainStrategy string                      `json:"domainStrategy"`
	Reserved       []int                       `json:"reserved"`
	Peers          []xrayWireguardOutboundPeer `json:"peers"`
}

type xrayWireguardOutboundPeer struct {
	PublicKey    string   `json:"publicKey"`
	PreSharedKey string   `json:"preSharedKey"`
	AllowedIPs   []string `json:"allowedIPs"`
	Endpoint     string   `json:"endpoint"`
	KeepAlive    int      `json:"keepAlive"`
}

// mapXrayOutbounds parses the source Xray outbounds and returns:
//   - WireGuard (WARP) outbounds converted to s-ui WARP endpoints,
//   - a map outboundTag -> s-ui routing target so MapXrayRouting can resolve
//     rules (blackhole->block, freedom->direct, wireguard->the endpoint tag),
//   - warnings for anything skipped.
//
// In 3x-ui, WARP is a wireguard *outbound* referenced by routing rules; s-ui
// models WARP as an *endpoint* and routes to it via Rules, so this is what lets
// the migrated WARP rule keep working instead of being flagged for review.
func mapXrayOutbounds(xrayConfig string) ([]model.Endpoint, map[string]string, []string) {
	targets := map[string]string{}
	if strings.TrimSpace(xrayConfig) == "" {
		return nil, targets, nil
	}
	var cfg struct {
		Outbounds []xrayOutbound `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(xrayConfig), &cfg); err != nil {
		return nil, targets, []string{fmt.Sprintf("routing: invalid xrayConfig outbounds: %v", err)}
	}
	var endpoints []model.Endpoint
	var warnings []string
	for _, ob := range cfg.Outbounds {
		tag := strings.TrimSpace(ob.Tag)
		if tag == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(ob.Protocol)) {
		case "blackhole":
			targets[tag] = "block"
		case "freedom":
			targets[tag] = "direct"
		case "wireguard":
			ep, w := warpEndpointFromOutbound(tag, ob.Settings)
			warnings = append(warnings, w...)
			if ep != nil {
				endpoints = append(endpoints, *ep)
				targets[tag] = tag // route to the endpoint by its own tag
			}
		default:
			// dns/proxy/etc. outbounds have no s-ui routing-target equivalent;
			// leave them unmapped so rules referencing them are flagged.
		}
	}
	return endpoints, targets, warnings
}

// warpEndpointFromOutbound converts an Xray wireguard outbound into an s-ui WARP
// endpoint (Type "warp" renders as a sing-box "wireguard" endpoint).
func warpEndpointFromOutbound(tag string, rawSettings json.RawMessage) (*model.Endpoint, []string) {
	var s xrayWireguardOutbound
	if err := json.Unmarshal(rawSettings, &s); err != nil {
		return nil, []string{fmt.Sprintf("warp outbound %s: invalid settings: %v", tag, err)}
	}
	if strings.TrimSpace(s.SecretKey) == "" {
		return nil, []string{fmt.Sprintf("warp outbound %s: missing secretKey; skipped", tag)}
	}
	if len(s.Peers) == 0 {
		return nil, []string{fmt.Sprintf("warp outbound %s: no peers; skipped", tag)}
	}
	mtu := s.MTU
	if mtu == 0 {
		mtu = 1420
	}
	var warnings []string
	// Xray carries reserved once for the whole outbound; sing-box puts it on the
	// peer. sing-box hard-rejects a reserved that is not exactly 3 bytes (0-255)
	// and that failure aborts the entire config parse, so drop a malformed one
	// rather than persist a config that will not load.
	var reserved []int
	if len(s.Reserved) > 0 {
		if validReserved(s.Reserved) {
			reserved = s.Reserved
		} else {
			warnings = append(warnings, fmt.Sprintf("warp outbound %s: reserved must be 3 bytes in 0-255; dropped", tag))
		}
	}
	peers := make([]map[string]any, 0, len(s.Peers))
	for _, p := range s.Peers {
		host, port := splitEndpointHostPort(p.Endpoint)
		allowed := p.AllowedIPs
		if len(allowed) == 0 {
			allowed = []string{"0.0.0.0/0", "::/0"}
		}
		peer := map[string]any{
			"address":     host,
			"port":        port,
			"public_key":  p.PublicKey,
			"allowed_ips": allowed,
		}
		if strings.TrimSpace(p.PreSharedKey) != "" {
			peer["pre_shared_key"] = p.PreSharedKey
		}
		if p.KeepAlive > 0 {
			peer["persistent_keepalive_interval"] = p.KeepAlive
		}
		if len(reserved) == 3 {
			peer["reserved"] = reserved
		}
		peers = append(peers, peer)
	}
	address := s.Address
	if address == nil {
		address = []string{}
	}
	if len(address) == 0 {
		warnings = append(warnings, fmt.Sprintf("warp outbound %s: no interface address; set one on the endpoint or it will not route", tag))
	}
	options := map[string]any{
		"address":     address,
		"private_key": s.SecretKey,
		"listen_port": 0,
		"mtu":         mtu,
		"peers":       peers,
	}
	if s.Workers > 0 {
		options["workers"] = s.Workers
	}
	if strings.TrimSpace(s.DomainStrategy) != "" {
		warnings = append(warnings, fmt.Sprintf("warp outbound %s: domainStrategy %q not carried over; set a domain resolver strategy on the endpoint if needed", tag, s.DomainStrategy))
	}
	optionsJSON, err := marshalJSON(options)
	if err != nil {
		return nil, []string{fmt.Sprintf("warp outbound %s: %v", tag, err)}
	}
	return &model.Endpoint{Type: "warp", Tag: tag, Options: optionsJSON}, warnings
}

// validReserved reports whether a WireGuard reserved value is exactly 3 bytes,
// each in 0-255 — the only shape sing-box accepts.
func validReserved(reserved []int) bool {
	if len(reserved) != 3 {
		return false
	}
	for _, v := range reserved {
		if v < 0 || v > 255 {
			return false
		}
	}
	return true
}

// splitEndpointHostPort splits "host:port" (or "[ipv6]:port") into host and
// port. A value without a port yields port 0.
func splitEndpointHostPort(endpoint string) (string, int) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", 0
	}
	host, portStr, err := net.SplitHostPort(endpoint)
	if err != nil {
		return endpoint, 0
	}
	port, _ := strconv.Atoi(portStr)
	return host, port
}
