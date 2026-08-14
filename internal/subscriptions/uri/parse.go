package uri

import (
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"net/url"
	"strconv"
	"strings"
)

func Parse(uri string, i int) (*map[string]interface{}, string, error) {
	u, err := url.Parse(uri)
	if err == nil {
		switch u.Scheme {
		case "vmess":
			return vmess(u.Host, i)
		case "vless":
			return vless(u, i)
		case "trojan":
			return trojan(u, i)
		case "hy", "hysteria":
			return hy(u, i)
		case "hy2", "hysteria2":
			return hy2(u, i)
		case "anytls":
			return anytls(u, i)
		case "tuic":
			return tuic(u, i)
		case "ss", "shadowsocks":
			return ss(u, i)
		case "naive+https", "naive+quic", "http2":
			return parseNaiveLink(u, i)
		}
	}
	return nil, "", common.NewError("Unsupported link format")
}

func parseEndpoint(u *url.URL, defaultPort int) (string, int, error) {
	if u == nil {
		return "", 0, common.NewError("missing link endpoint")
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return "", 0, common.NewError("missing link host")
	}
	port := defaultPort
	if rawPort := u.Port(); rawPort != "" {
		parsed, err := strconv.Atoi(rawPort)
		if err != nil || parsed < 1 || parsed > 65535 {
			return "", 0, common.NewError("invalid link port")
		}
		port = parsed
	}
	if port < 1 || port > 65535 {
		return "", 0, common.NewError("invalid link port")
	}
	return host, port, nil
}

func requiredUsername(u *url.URL, label string) (string, error) {
	if u == nil || u.User == nil || strings.TrimSpace(u.User.Username()) == "" {
		return "", common.NewError("missing " + label)
	}
	return u.User.Username(), nil
}

func parsePortValue(value any) (int, error) {
	var port int64
	switch typed := value.(type) {
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 32)
		if err != nil {
			return 0, common.NewError("invalid link port")
		}
		port = parsed
	case float64:
		port = int64(typed)
		if float64(port) != typed {
			return 0, common.NewError("invalid link port")
		}
	default:
		return 0, common.NewError("invalid link port")
	}
	if port < 1 || port > 65535 {
		return 0, common.NewError("invalid link port")
	}
	return int(port), nil
}

func truthyJSONValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed == 1
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return err == nil && parsed
	default:
		return false
	}
}
