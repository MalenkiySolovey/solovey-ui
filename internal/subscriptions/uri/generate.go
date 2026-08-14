package uri

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

var SupportedInboundTypes = []string{"socks", "http", "mixed", "shadowsocks", "naive", "hysteria", "hysteria2", "anytls", "tuic", "vless", "trojan", "vmess"}

type LinkParam struct {
	Key   string
	Value string
}

const defaultTUICUDPRelayMode = "quic"

// mapString returns m[key] as a string, or "" when the key is absent or not a
// string. Inbound/addr maps come from operator-supplied or imported config and
// may be malformed; using these accessors keeps a bad value from panicking the
// subscription request goroutine.
func mapString(m map[string]interface{}, key string) string {
	s, _ := m[key].(string)
	return s
}

// asBool returns v as a bool, or false when v is nil or not a bool.
func asBool(v interface{}) bool {
	b, _ := v.(bool)
	return b
}
func Generate(clientConfig json.RawMessage, i *model.Inbound, hostname string) ([]string, error) {
	if i == nil {
		return nil, errors.New("subscription inbound is required")
	}
	inbound, err := i.MarshalFull()
	if err != nil {
		return nil, fmt.Errorf("encode subscription inbound: %w", err)
	}
	var tls map[string]interface{}
	if i.TlsId > 0 {
		tls, err = prepareTls(i.Tls)
		if err != nil {
			return nil, err
		}
	}
	var userConfig map[string]map[string]interface{}
	if err := json.Unmarshal(clientConfig, &userConfig); err != nil {
		return nil, fmt.Errorf("decode subscription client config: %w", err)
	}
	Addrs, err := decodeAddresses(i.Addrs)
	if err != nil {
		return nil, err
	}
	if len(Addrs) == 0 {
		Addrs = append(Addrs, map[string]interface{}{
			"server":      hostname,
			"server_port": (*inbound)["listen_port"],
			"remark":      i.Tag,
		})
		if i.TlsId > 0 {
			Addrs[0]["tls"] = tls
		}
	} else {
		for index, addr := range Addrs {
			addrRemark, _ := addr["remark"].(string)
			Addrs[index]["remark"] = i.Tag + addrRemark
			if i.TlsId > 0 {
				newTls := map[string]interface{}{}
				for k, v := range tls {
					newTls[k] = v
				}
				// Override tls
				if addrTls, ok := addr["tls"].(map[string]interface{}); ok {
					for k, v := range addrTls {
						newTls[k] = v
					}
				}
				Addrs[index]["tls"] = newTls
			}
		}
	}
	if err := validateLinkAddresses(Addrs); err != nil {
		return nil, err
	}
	var links []string
	switch i.Type {
	case "socks":
		links = socksLink(userConfig["socks"], Addrs)
	case "http":
		links = httpLink(userConfig["http"], Addrs)
	case "mixed":
		links = append(
			socksLink(userConfig["socks"], Addrs),
			httpLink(userConfig["http"], Addrs)...,
		)
	case "shadowsocks":
		links = shadowsocksLink(userConfig, *inbound, Addrs)
	case "naive":
		links = naiveLink(userConfig["naive"], *inbound, Addrs)
	case "hysteria":
		links = hysteriaLink(userConfig["hysteria"], *inbound, Addrs)
	case "hysteria2":
		links = hysteria2Link(userConfig["hysteria2"], *inbound, Addrs)
	case "tuic":
		links = tuicLink(userConfig["tuic"], *inbound, Addrs)
	case "vless":
		links = vlessLink(userConfig["vless"], *inbound, Addrs)
	case "anytls":
		links = anytlsLink(userConfig["anytls"], Addrs)
	case "trojan":
		links = trojanLink(userConfig["trojan"], *inbound, Addrs)
	case "vmess":
		links = vmessLink(userConfig["vmess"], *inbound, Addrs)
	default:
		return nil, fmt.Errorf("unsupported subscription inbound type %q", i.Type)
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("subscription inbound %q produced no links", i.Tag)
	}
	for index, link := range links {
		if err := validateGeneratedLink(link); err != nil {
			return nil, fmt.Errorf("validate generated subscription link %d for inbound %q: %w", index+1, i.Tag, err)
		}
	}
	return links, nil
}

func validateGeneratedLink(link string) error {
	parsed, err := url.Parse(link)
	if err != nil {
		return err
	}
	switch parsed.Scheme {
	case "socks5", "http", "https":
		password, hasPassword := parsed.User.Password()
		if parsed.User == nil || parsed.User.Username() == "" || !hasPassword || password == "" || parsed.Hostname() == "" {
			return errors.New("standard proxy link is missing credentials or host")
		}
		port, err := strconv.Atoi(parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return errors.New("standard proxy link has an invalid port")
		}
		return nil
	default:
		_, _, err := Parse(link, 0)
		return err
	}
}

func validateLinkAddresses(addresses []map[string]interface{}) error {
	for index, address := range addresses {
		host, ok := address["server"].(string)
		host = strings.TrimSpace(host)
		if !ok || host == "" {
			return fmt.Errorf("subscription address %d has no server", index+1)
		}
		if strings.Contains(host, ":") {
			ip, err := netip.ParseAddr(strings.Trim(host, "[]"))
			if err != nil || !ip.Is6() {
				return fmt.Errorf("subscription address %d has an invalid server", index+1)
			}
			host = "[" + ip.String() + "]"
		}
		port, ok := address["server_port"].(float64)
		if !ok || math.Trunc(port) != port || port < 1 || port > 65535 {
			return fmt.Errorf("subscription address %d has an invalid port", index+1)
		}
		address["server"] = host
	}
	return nil
}

// ValidateAddresses verifies explicitly persisted subscription address rows.
// An empty/null list is valid and means the caller will use its configured
// hostname and listen port fallback.
func ValidateAddresses(raw json.RawMessage) error {
	addresses, err := decodeAddresses(raw)
	if err != nil {
		return err
	}
	return validateLinkAddresses(addresses)
}

func decodeAddresses(raw json.RawMessage) ([]map[string]interface{}, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var addresses []map[string]interface{}
	if err := json.Unmarshal(raw, &addresses); err != nil {
		return nil, fmt.Errorf("decode subscription inbound addresses: %w", err)
	}
	return addresses, nil
}

func prepareTls(t *model.Tls) (map[string]interface{}, error) {
	if t == nil {
		return nil, errors.New("subscription TLS configuration is unavailable")
	}
	var iTls, oTls map[string]interface{}
	if err := json.Unmarshal(t.Client, &oTls); err != nil {
		return nil, fmt.Errorf("decode subscription TLS client: %w", err)
	}
	if err := json.Unmarshal(t.Server, &iTls); err != nil {
		return nil, fmt.Errorf("decode subscription TLS server: %w", err)
	}
	if iTls == nil || oTls == nil {
		return nil, errors.New("subscription TLS configuration must contain objects")
	}
	for k, v := range iTls {
		switch k {
		case "enabled", "server_name", "alpn":
			oTls[k] = v
		case "reality":
			reality, okReality := v.(map[string]interface{})
			clientReality, okClient := oTls["reality"].(map[string]interface{})
			if !okReality || !okClient {
				continue
			}
			clientReality["enabled"] = reality["enabled"]
			if shortIDs, hasSIds := reality["short_id"].([]interface{}); hasSIds && len(shortIDs) > 0 {
				clientReality["short_id"] = shortIDs[common.RandomInt(len(shortIDs))]
			}
			oTls["reality"] = clientReality
		}
	}
	return oTls, nil
}
func toBase64(d []byte) string {
	return base64.StdEncoding.EncodeToString(d)
}
