package endpoints

import (
	"crypto/ecdh"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

var ErrPeerConfigUnavailable = errors.New("endpoint_peer_config_unavailable")

type PeerConfig struct {
	Peer   int    `json:"peer"`
	Config string `json:"config"`
}

// PeerConfigs is an explicit private delivery operation over one canonical row.
// It also works for preserved desired state: export does not activate a runtime.
func PeerConfigs(db *gorm.DB, id uint, host string) ([]PeerConfig, error) {
	if db == nil || id == 0 || !peerConfigHost(host) {
		return nil, ErrPeerConfigUnavailable
	}
	var result []PeerConfig
	err := db.Transaction(func(tx *gorm.DB) error {
		var endpoint model.Endpoint
		if tx.First(&endpoint, id).Error != nil || endpoint.Type != "wireguard" || len(endpoint.Options)+len(endpoint.Ext) > 1<<20 {
			return ErrPeerConfigUnavailable
		}
		raw, err := marshalEndpointCredentials(tx, endpoint, "endpoint-peer-config")
		if err != nil {
			return ErrPeerConfigUnavailable
		}
		defer clear(raw)
		result, err = renderPeerConfigs(raw, endpoint.Ext, host)
		return err
	})
	if err != nil {
		return nil, ErrPeerConfigUnavailable
	}
	return result, nil
}

func peerConfigHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return address.Zone() == ""
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func peerConfigPublicKey(encoded string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return "", ErrPeerConfigUnavailable
	}
	defer clear(key)
	private, err := ecdh.X25519().NewPrivateKey(key)
	if err != nil {
		return "", ErrPeerConfigUnavailable
	}
	return base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()), nil
}

func renderPeerConfigs(raw, extension []byte, host string) ([]PeerConfig, error) {
	var options struct {
		PrivateKey string                     `json:"private_key"`
		ListenPort uint16                     `json:"listen_port"`
		MTU        uint32                     `json:"mtu"`
		Amnezia    map[string]json.RawMessage `json:"amnezia"`
		Peers      []struct {
			PublicKey    string   `json:"public_key"`
			PresharedKey string   `json:"pre_shared_key"`
			AllowedIPs   []string `json:"allowed_ips"`
			Keepalive    uint16   `json:"persistent_keepalive_interval"`
		} `json:"peers"`
	}
	var ext struct {
		Keys []struct {
			PublicKey  string `json:"public_key"`
			PrivateKey string `json:"private_key"`
		} `json:"keys"`
	}
	if json.Unmarshal(raw, &options) != nil || json.Unmarshal(extension, &ext) != nil || options.ListenPort == 0 || len(options.Peers) > 128 || len(ext.Keys) > 128 {
		return nil, ErrPeerConfigUnavailable
	}
	policy, dns, err := peerDeliveryPolicy(extension)
	if err != nil {
		return nil, err
	}
	serverPublic, err := peerConfigPublicKey(options.PrivateKey)
	if err != nil {
		return nil, err
	}
	var amnezia strings.Builder
	if err := writePeerAmnezia(&amnezia, options.Amnezia); err != nil {
		return nil, err
	}
	publicEndpoint := policy.PublicEndpoint
	if publicEndpoint == "" {
		publicEndpoint = net.JoinHostPort(host, strconv.Itoa(int(options.ListenPort)))
	}
	allowedIPs := policy.ClientAllowedIPs
	if allowedIPs == nil {
		allowedIPs = []string{"0.0.0.0/0", "::/0"}
	}
	keys := make(map[string]string, len(ext.Keys))
	for _, key := range ext.Keys {
		if _, duplicate := keys[key.PublicKey]; duplicate {
			return nil, ErrPeerConfigUnavailable
		}
		public, err := peerConfigPublicKey(key.PrivateKey)
		if err != nil || subtle.ConstantTimeCompare([]byte(public), []byte(key.PublicKey)) != 1 {
			return nil, ErrPeerConfigUnavailable
		}
		keys[key.PublicKey] = key.PrivateKey
	}
	var result []PeerConfig
	total := 0
	for i, peer := range options.Peers {
		private, owned := keys[peer.PublicKey]
		if !owned { // An externally managed peer has no private client config here.
			continue
		}
		if len(peer.AllowedIPs) == 0 || len(peer.AllowedIPs) > 128 {
			return nil, ErrPeerConfigUnavailable
		}
		for _, prefix := range peer.AllowedIPs {
			if _, err := netip.ParsePrefix(prefix); err != nil {
				return nil, ErrPeerConfigUnavailable
			}
		}
		var config strings.Builder
		config.WriteString("[Interface]\nPrivateKey = " + private + "\nAddress = " + strings.Join(peer.AllowedIPs, ", ") + "\n")
		if dns != "" {
			config.WriteString("DNS = " + dns + "\n")
		}
		if options.MTU != 0 {
			config.WriteString("MTU = " + strconv.FormatUint(uint64(options.MTU), 10) + "\n")
		}
		config.WriteString(amnezia.String())
		config.WriteString("\n[Peer]\nPublicKey = " + serverPublic + "\nAllowedIPs = " + strings.Join(allowedIPs, ", ") + "\nEndpoint = " + publicEndpoint + "\n")
		if peer.PresharedKey != "" {
			key, err := base64.StdEncoding.DecodeString(peer.PresharedKey)
			if err != nil || len(key) != 32 {
				return nil, ErrPeerConfigUnavailable
			}
			clear(key)
			config.WriteString("PresharedKey = " + peer.PresharedKey + "\n")
		}
		keepalive := peer.Keepalive
		if policy.ClientKeepalive != nil {
			keepalive = *policy.ClientKeepalive
		}
		if keepalive != 0 {
			config.WriteString("PersistentKeepalive = " + strconv.Itoa(int(keepalive)) + "\n")
		}
		total += config.Len()
		if total > 1<<20 {
			return nil, ErrPeerConfigUnavailable
		}
		result = append(result, PeerConfig{Peer: i + 1, Config: config.String()})
	}
	if len(result) == 0 {
		return nil, ErrPeerConfigUnavailable
	}
	return result, nil
}

func peerConfigLine(value string) bool {
	if len(value) > 8192 {
		return false
	}
	for _, c := range value {
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}

func writePeerAmnezia(out *strings.Builder, fields map[string]json.RawMessage) error {
	// Fixed native schema: unknown fields remain stored, but cannot be silently
	// omitted from a supposedly complete client configuration.
	known := map[string]bool{}
	for _, item := range []struct{ key, name, kind string }{
		{"jc", "Jc", "number"}, {"jmin", "Jmin", "number"}, {"jmax", "Jmax", "number"},
		{"s1", "S1", "number"}, {"s2", "S2", "number"}, {"s3", "S3", "number"}, {"s4", "S4", "number"},
		{"h1", "H1", "range"}, {"h2", "H2", "range"}, {"h3", "H3", "range"}, {"h4", "H4", "range"},
		{"i1", "I1", "text"}, {"i2", "I2", "text"}, {"i3", "I3", "text"}, {"i4", "I4", "text"}, {"i5", "I5", "text"},
		{"header_protection_key", "HeaderProtectionKey", "key"},
		{"content_padding_addition", "ContentPaddingAddition", "range"}, {"rekey_after_time", "RekeyAfterTime", "range"},
		{"rekey_timeout", "RekeyTimeout", "range"}, {"reject_after_time", "RejectAfterTime", "range"},
		{"keepalive_timeout", "KeepaliveTimeout", "range"}, {"max_handshake_attempts", "MaxHandshakeAttempts", "range"},
		{"random_trailers", "RandomTrailers", "bool"}, {"disable_cookies", "DisableCookies", "bool"},
	} {
		known[item.key] = true
		raw, present := fields[item.key]
		if !present {
			continue
		}
		var value string
		if item.kind == "bool" {
			var enabled bool
			if string(raw) == "null" || json.Unmarshal(raw, &enabled) != nil {
				return ErrPeerConfigUnavailable
			}
			value = strconv.FormatBool(enabled)
		} else {
			if json.Unmarshal(raw, &value) != nil {
				if item.kind != "number" && item.kind != "range" {
					return ErrPeerConfigUnavailable
				}
				value = string(raw)
			}
			if !peerConfigLine(value) {
				return ErrPeerConfigUnavailable
			}
			switch item.kind {
			case "number", "range":
				parts := strings.Split(value, "-")
				if len(parts) > 2 || item.kind == "number" && len(parts) != 1 {
					return ErrPeerConfigUnavailable
				}
				var first uint64
				for i, part := range parts {
					n, err := strconv.ParseUint(part, 10, 32)
					if err != nil || i == 1 && n < first {
						return ErrPeerConfigUnavailable
					}
					first = n
				}
			case "key":
				key, err := base64.StdEncoding.DecodeString(value)
				if err != nil || len(key) != 32 {
					return ErrPeerConfigUnavailable
				}
				clear(key)
			}
		}
		if value != "" {
			out.WriteString(item.name + " = " + value + "\n")
		}
	}
	for key := range fields {
		if !known[key] {
			return ErrPeerConfigUnavailable
		}
	}
	return nil
}
