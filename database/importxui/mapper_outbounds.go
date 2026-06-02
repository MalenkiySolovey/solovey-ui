package importxui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/deposist/s-ui-x/database/model"
)

// dnsHijackTarget is the sentinel routing target for an Xray `dns` outbound.
// sing-box has no `dns` *outbound* (s-ui's OutboundRegistry registers none);
// the equivalent is a route rule with action "hijack-dns", which MapXrayRouting
// emits when a rule resolves to this target.
const dnsHijackTarget = "hijack-dns"

// xrayProxySettings is the `settings` block of an Xray *outbound*. vmess/vless
// use `vnext`; trojan/shadowsocks/socks/http use `servers`. Only the first
// server is migrated (sing-box outbounds target a single server).
type xrayProxySettings struct {
	Vnext   []xrayProxyServer `json:"vnext"`
	Servers []xrayProxyServer `json:"servers"`
}

type xrayProxyServer struct {
	Address  string          `json:"address"`
	Port     int             `json:"port"`
	Password string          `json:"password"` // trojan, shadowsocks
	Method   string          `json:"method"`   // shadowsocks
	Users    []xrayProxyUser `json:"users"`
}

type xrayProxyUser struct {
	ID         string `json:"id"`         // vmess, vless
	AlterID    int    `json:"alterId"`    // vmess
	Security   string `json:"security"`   // vmess
	Flow       string `json:"flow"`       // vless
	Encryption string `json:"encryption"` // vless (none)
	User       string `json:"user"`       // socks, http
	Pass       string `json:"pass"`       // socks, http
}

// xuiTLSSetting is the `tlsSettings` block of an Xray stream (client/outbound
// side). Only the fields s-ui can carry over are decoded.
type xuiTLSSetting struct {
	ServerName    string   `json:"serverName"`
	AllowInsecure bool     `json:"allowInsecure"`
	Fingerprint   string   `json:"fingerprint"`
	ALPN          []string `json:"alpn"`
}

// outboundFromXray converts a single proxy Xray outbound (vmess/vless/trojan/
// shadowsocks/socks/http) into an s-ui (sing-box) outbound. It returns nil with
// a warning for protocols that have no automatic mapping or for malformed
// settings, so the caller can surface the loss instead of dropping it silently.
func outboundFromXray(ob xrayOutbound) (*model.Outbound, []string) {
	proto := strings.ToLower(strings.TrimSpace(ob.Protocol))
	tag := strings.TrimSpace(ob.Tag)
	var settings xrayProxySettings
	if err := decodeJSON(ob.Settings, &settings); err != nil {
		return nil, []string{fmt.Sprintf("outbound %s: invalid %s settings: %v; skipped", tag, proto, err)}
	}
	stream := parseOutboundStream(ob)

	opts := map[string]any{}
	var warnings []string
	var server string
	carriesStream := false // vmess/vless/trojan/http carry tls + transport

	switch proto {
	case "vmess":
		ep := firstServer(settings.Vnext)
		if ep == nil {
			return nil, []string{fmt.Sprintf("outbound %s: vmess has no vnext server; skipped", tag)}
		}
		user := firstProxyUser(ep.Users)
		server = strings.TrimSpace(ep.Address)
		opts["server"] = server
		opts["server_port"] = ep.Port
		opts["uuid"] = strings.TrimSpace(user.ID)
		opts["security"] = firstNonEmpty(user.Security, "auto")
		opts["alter_id"] = user.AlterID
		carriesStream = true
	case "vless":
		ep := firstServer(settings.Vnext)
		if ep == nil {
			return nil, []string{fmt.Sprintf("outbound %s: vless has no vnext server; skipped", tag)}
		}
		user := firstProxyUser(ep.Users)
		server = strings.TrimSpace(ep.Address)
		opts["server"] = server
		opts["server_port"] = ep.Port
		opts["uuid"] = strings.TrimSpace(user.ID)
		if flow := strings.TrimSpace(user.Flow); flow != "" {
			opts["flow"] = flow
		}
		carriesStream = true
	case "trojan":
		srv := firstServer(settings.Servers)
		if srv == nil {
			return nil, []string{fmt.Sprintf("outbound %s: trojan has no servers; skipped", tag)}
		}
		server = strings.TrimSpace(srv.Address)
		opts["server"] = server
		opts["server_port"] = srv.Port
		opts["password"] = srv.Password
		carriesStream = true
	case "shadowsocks":
		srv := firstServer(settings.Servers)
		if srv == nil {
			return nil, []string{fmt.Sprintf("outbound %s: shadowsocks has no servers; skipped", tag)}
		}
		server = strings.TrimSpace(srv.Address)
		opts["server"] = server
		opts["server_port"] = srv.Port
		opts["method"] = firstNonEmpty(srv.Method, "none")
		opts["password"] = srv.Password
		// shadowsocks has no tls/transport in sing-box.
	case "socks":
		srv := firstServer(settings.Servers)
		if srv == nil {
			return nil, []string{fmt.Sprintf("outbound %s: socks has no servers; skipped", tag)}
		}
		server = strings.TrimSpace(srv.Address)
		opts["server"] = server
		opts["server_port"] = srv.Port
		opts["version"] = "5"
		if user := firstProxyUser(srv.Users); strings.TrimSpace(user.User) != "" {
			opts["username"] = strings.TrimSpace(user.User)
			opts["password"] = user.Pass
		}
	case "http":
		srv := firstServer(settings.Servers)
		if srv == nil {
			return nil, []string{fmt.Sprintf("outbound %s: http has no servers; skipped", tag)}
		}
		server = strings.TrimSpace(srv.Address)
		opts["server"] = server
		opts["server_port"] = srv.Port
		if user := firstProxyUser(srv.Users); strings.TrimSpace(user.User) != "" {
			opts["username"] = strings.TrimSpace(user.User)
			opts["password"] = user.Pass
		}
		carriesStream = true
	default:
		return nil, []string{fmt.Sprintf("outbound %s: protocol %q has no automatic s-ui mapping; recreate it manually", tag, proto)}
	}

	if server == "" {
		return nil, []string{fmt.Sprintf("outbound %s: missing server address; skipped", tag)}
	}

	if carriesStream {
		if tls, tlsWarn := mapOutboundClientTLS(tag, stream); tls != nil {
			opts["tls"] = tls
			warnings = append(warnings, tlsWarn...)
		} else {
			warnings = append(warnings, tlsWarn...)
		}
		if transport, trWarn := mapTransport("outbound", tag, stream); transport != nil {
			opts["transport"] = transport
			warnings = append(warnings, trWarn...)
		} else {
			warnings = append(warnings, trWarn...)
		}
	}

	optionsJSON, err := marshalJSON(opts)
	if err != nil {
		return nil, append(warnings, fmt.Sprintf("outbound %s: %v", tag, err))
	}
	return &model.Outbound{Type: proto, Tag: tag, Options: optionsJSON}, warnings
}

func firstServer(servers []xrayProxyServer) *xrayProxyServer {
	if len(servers) == 0 {
		return nil
	}
	return &servers[0]
}

func firstProxyUser(users []xrayProxyUser) xrayProxyUser {
	if len(users) == 0 {
		return xrayProxyUser{}
	}
	return users[0]
}

// parseOutboundStream decodes an Xray outbound's streamSettings into the shared
// xuiStreamSettings shape so mapTransport and mapOutboundClientTLS can reuse the
// inbound helpers. An absent/invalid block yields a zero (tcp/none) stream.
func parseOutboundStream(ob xrayOutbound) xuiStreamSettings {
	var stream xuiStreamSettings
	if len(ob.StreamSettings) == 0 {
		return stream
	}
	if err := json.Unmarshal(ob.StreamSettings, &stream); err != nil {
		return xuiStreamSettings{}
	}
	stream.Network = strings.ToLower(strings.TrimSpace(stream.Network))
	stream.Security = strings.ToLower(strings.TrimSpace(stream.Security))
	return stream
}

// mapOutboundClientTLS builds the sing-box outbound `tls` block from an Xray
// outbound's streamSettings. For reality it uses the peer public key/short id
// that the Xray outbound stores at the top level of realitySettings (unlike an
// inbound, which stores the private key). Returns nil when TLS is disabled.
func mapOutboundClientTLS(tag string, stream xuiStreamSettings) (map[string]any, []string) {
	switch stream.Security {
	case "", "none":
		return nil, nil
	case "tls":
		tls := map[string]any{"enabled": true}
		if sni := strings.TrimSpace(stream.TLSSettings.ServerName); sni != "" {
			tls["server_name"] = sni
		}
		if stream.TLSSettings.AllowInsecure {
			tls["insecure"] = true
		}
		if len(stream.TLSSettings.ALPN) > 0 {
			tls["alpn"] = stream.TLSSettings.ALPN
		}
		if fp := strings.TrimSpace(stream.TLSSettings.Fingerprint); fp != "" {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
		return tls, nil
	case "reality":
		r := stream.RealitySettings
		serverName := firstNonEmpty(r.ServerName, firstString(r.ServerNames))
		fingerprint := firstNonEmpty(r.Fingerprint, "chrome")
		var warnings []string
		if strings.TrimSpace(r.PublicKey) == "" {
			warnings = append(warnings, fmt.Sprintf("outbound %s: reality publicKey is empty; verify the outbound TLS settings", tag))
		}
		tls := map[string]any{
			"enabled":     true,
			"server_name": serverName,
			"utls": map[string]any{
				"enabled":     true,
				"fingerprint": fingerprint,
			},
			"reality": map[string]any{
				"enabled":    true,
				"public_key": strings.TrimSpace(r.PublicKey),
				"short_id":   firstNonEmpty(r.ShortID, firstString(r.ShortIDs)),
			},
		}
		return tls, warnings
	default:
		return nil, []string{fmt.Sprintf("outbound %s: TLS security %q requires manual review", tag, stream.Security)}
	}
}
