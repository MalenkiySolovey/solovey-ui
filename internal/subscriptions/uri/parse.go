package uri

import (
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

func Parse(uri string, i int) (*map[string]interface{}, string, error) {
	if !utf8.ValidString(uri) {
		return nil, "", common.NewError("Invalid link encoding")
	}
	if scheme, payload, found := strings.Cut(uri, "://"); found && strings.EqualFold(scheme, "vmess") {
		if strings.ContainsAny(payload, "?#") {
			return nil, "", common.NewError("Invalid vmess payload")
		}
		return vmess(payload, i)
	}
	u, err := url.Parse(uri)
	if err == nil {
		query, queryErr := url.ParseQuery(u.RawQuery)
		if queryErr != nil {
			return nil, "", common.NewError("Invalid link query")
		}
		for key, values := range query {
			if !utf8.ValidString(key) || len(values) > 1 {
				return nil, "", common.NewError("Invalid or ambiguous link query")
			}
			for _, value := range values {
				if !utf8.ValidString(value) {
					return nil, "", common.NewError("Invalid link query encoding")
				}
			}
		}
		u.Scheme = strings.ToLower(u.Scheme)
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
		case "socks5", "http", "https":
			return parseStandardProxy(u, i)
		}
	}
	return nil, "", common.NewError("Unsupported link format")
}

func parseEndpoint(u *url.URL, defaultPort int) (string, int, error) {
	if u == nil {
		return "", 0, common.NewError("missing link endpoint")
	}
	host, err := codec.NormalizeHost(u.Hostname())
	if err != nil {
		return "", 0, common.NewError("missing link host")
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(u.Host, "[") {
		return "", 0, common.NewError("IPv6 link authority requires brackets")
	}
	port := defaultPort
	if strings.HasSuffix(u.Host, ":") {
		return "", 0, common.NewError("invalid empty link port")
	}
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
	if u == nil || u.User == nil || strings.TrimSpace(u.User.Username()) == "" || !utf8.ValidString(u.User.Username()) {
		return "", common.NewError("missing " + label)
	}
	return u.User.Username(), nil
}

func requiredSingleCredential(u *url.URL, label string) (string, error) {
	value, err := requiredUsername(u, label)
	if err != nil {
		return "", err
	}
	if _, extra := u.User.Password(); extra {
		return "", common.NewError("invalid " + label + " userinfo shape")
	}
	return value, nil
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
