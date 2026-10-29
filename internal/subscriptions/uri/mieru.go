package uri

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

// The simple share format follows enfein/mieru v3.33.0 pkg/appctl/url.go.
// Keep this portable adapter independent of either Provider's engine graph.
func parseMieru(u *url.URL, index int) (*map[string]any, string, error) {
	invalid := func() (*map[string]any, string, error) { return nil, "", common.NewError("invalid Mieru share link") }
	if u == nil || u.User == nil || u.User.Username() == "" || u.Port() != "" || u.Path != "" {
		return invalid()
	}
	password, present := u.User.Password()
	if !present || password == "" {
		return invalid()
	}
	host, err := codec.NormalizeHost(u.Hostname())
	if err != nil {
		return invalid()
	}
	q := u.Query()
	if q.Get("profile") == "" {
		return invalid()
	}
	ports, protocols := q["port"], q["protocol"]
	if len(ports) == 0 || len(ports) != len(protocols) {
		return invalid()
	}
	transport := protocols[0]
	if transport != "TCP" && transport != "UDP" {
		return invalid()
	}
	raw := map[string]any{"type": "mieru", "server": host, "username": u.User.Username(), "password": password, "transport": transport}
	var ranges []string
	for i, token := range ports {
		if protocols[i] != transport {
			return invalid()
		} // SDK has one carrier per endpoint.
		normalized, first, single, err := mieruPort(token)
		if err != nil {
			return invalid()
		}
		if i == 0 && single {
			raw["server_port"] = first
		} else {
			ranges = append(ranges, normalized)
		}
	}
	if len(ranges) > 0 {
		raw["server_ports"] = ranges
	}
	for query, field := range map[string]string{"multiplexing": "multiplexing", "handshake-mode": "handshake_mode", "traffic-pattern": "traffic_pattern"} {
		if value := q.Get(query); value != "" {
			raw[field] = value
		}
	}
	if value := q.Get("mtu"); value != "" {
		mtu, err := strconv.Atoi(value)
		if err != nil || mtu < 0 || mtu > 65535 {
			return invalid()
		}
		raw["mtu"] = mtu
	}
	if !validMieruOptions(raw) {
		return invalid()
	}
	tag := u.Fragment
	if tag == "" {
		tag = q.Get("profile")
	}
	if index > 0 {
		tag = fmt.Sprintf("%d.%s", index, tag)
	}
	raw["tag"] = tag
	return &raw, tag, nil
}

func mieruPort(token string) (normalized string, first int, single bool, err error) {
	parts := strings.Split(strings.ReplaceAll(token, ":", "-"), "-")
	if len(parts) < 1 || len(parts) > 2 {
		return "", 0, false, canonical.ErrUnsupportedProjection
	}
	first, err = strconv.Atoi(parts[0])
	if err != nil || first < 1 || first > 65535 {
		return "", 0, false, canonical.ErrUnsupportedProjection
	}
	last := first
	if len(parts) == 2 {
		last, err = strconv.Atoi(parts[1])
	}
	if err != nil || last < first || last > 65535 {
		return "", 0, false, canonical.ErrUnsupportedProjection
	}
	return fmt.Sprintf("%d-%d", first, last), first, len(parts) == 1, nil
}

func validMieruOptions(raw map[string]any) bool {
	for _, field := range []string{"multiplexing", "handshake_mode", "traffic_pattern"} {
		if value, present := raw[field]; present {
			if _, ok := value.(string); !ok {
				return false
			}
		}
	}
	if value := mapString(raw, "multiplexing"); value != "" {
		switch value {
		case "MULTIPLEXING_DEFAULT", "MULTIPLEXING_OFF", "MULTIPLEXING_LOW", "MULTIPLEXING_MIDDLE", "MULTIPLEXING_HIGH":
		default:
			return false
		}
	}
	if value := mapString(raw, "handshake_mode"); value != "" {
		switch value {
		case "HANDSHAKE_DEFAULT", "HANDSHAKE_STANDARD", "HANDSHAKE_NO_WAIT":
		default:
			return false
		}
	}
	if value := mapString(raw, "traffic_pattern"); value != "" {
		if _, err := base64.StdEncoding.DecodeString(value); err != nil {
			return false
		}
	}
	return true
}

func renderMieru(raw map[string]any, label string) ([]string, error) {
	username, password := mapString(raw, "username"), mapString(raw, "password")
	transport := mapString(raw, "transport")
	if username == "" || password == "" || (transport != "TCP" && transport != "UDP") || !validMieruOptions(raw) {
		return nil, canonical.ErrUnsupportedProjection
	}
	host, err := codec.NormalizeHost(mapString(raw, "server"))
	if err != nil {
		return nil, canonical.ErrUnsupportedProjection
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u := &url.URL{Scheme: "mierus", Host: host, User: url.UserPassword(username, password)}
	q := url.Values{}
	if label == "" {
		label = "mieru"
	}
	q.Set("profile", label)
	if value, present := raw["server_port"]; present && value != nil && value != float64(0) && value != 0 {
		port, err := parsePortValue(value)
		if err != nil {
			return nil, canonical.ErrUnsupportedProjection
		}
		q.Add("port", strconv.Itoa(port))
		q.Add("protocol", transport)
	}
	if value, present := raw["server_ports"]; present {
		var ranges []string
		switch ports := value.(type) {
		case string:
			ranges = []string{ports}
		case []string:
			ranges = ports
		case []any:
			for _, v := range ports {
				s, ok := v.(string)
				if !ok {
					return nil, canonical.ErrUnsupportedProjection
				}
				ranges = append(ranges, s)
			}
		default:
			return nil, canonical.ErrUnsupportedProjection
		}
		for _, token := range ranges {
			normalized, _, _, err := mieruPort(token)
			if err != nil {
				return nil, err
			}
			q.Add("port", normalized)
			q.Add("protocol", transport)
		}
	}
	if len(q["port"]) == 0 {
		return nil, canonical.ErrUnsupportedProjection
	}
	for field, query := range map[string]string{"multiplexing": "multiplexing", "handshake_mode": "handshake-mode", "traffic_pattern": "traffic-pattern"} {
		if value := mapString(raw, field); value != "" {
			q.Set(query, value)
		}
	}
	if value, present := raw["mtu"]; present {
		mtu, err := strconv.Atoi(fmt.Sprint(value))
		if err != nil || mtu < 0 || mtu > 65535 {
			return nil, canonical.ErrUnsupportedProjection
		}
		q.Set("mtu", strconv.Itoa(mtu))
	}
	u.RawQuery = q.Encode()
	return []string{u.String()}, nil
}
