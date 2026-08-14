package uri

import (
	"fmt"
	"net/url"
	"strings"

	uricodec "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func anytls(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, "", common.NewError("invalid anytls query")
	}
	host, port, err := parseEndpoint(u, 443)
	if err != nil {
		return nil, "", err
	}
	password, err := requiredUsername(u, "anytls password")
	if err != nil {
		return nil, "", err
	}
	security := query.Get("security")
	if len(security) == 0 {
		security = "tls"
	}
	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	anytls := map[string]interface{}{
		"type":        "anytls",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"password":    password,
		"tls":         getTls(security, &query),
	}
	return &anytls, tag, nil
}
func tuic(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, "", common.NewError("invalid tuic query")
	}
	host, port, err := parseEndpoint(u, 443)
	if err != nil {
		return nil, "", err
	}
	uuid, err := requiredUsername(u, "tuic uuid")
	if err != nil {
		return nil, "", err
	}
	security := query.Get("security")
	if len(security) == 0 {
		security = "tls"
	}
	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	password, hasPassword := u.User.Password()
	if !hasPassword || password == "" {
		return nil, "", common.NewError("missing tuic password")
	}
	tuic := map[string]interface{}{
		"type":               "tuic",
		"tag":                tag,
		"server":             host,
		"server_port":        port,
		"uuid":               uuid,
		"password":           password,
		"congestion_control": query.Get("congestion_control"),
		"udp_relay_mode":     query.Get("udp_relay_mode"),
		"tls":                getTls(security, &query),
	}
	return &tuic, tag, nil
}
func ss(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, "", common.NewError("invalid shadowsocks query")
	}
	host, port, err := parseEndpoint(u, 443)
	if err != nil {
		return nil, "", err
	}
	method, err := requiredUsername(u, "shadowsocks method")
	if err != nil {
		return nil, "", err
	}
	password, ok := u.User.Password()
	if !ok {
		decrypted := uricodec.DecodeOrOriginal(method)
		decrypted_arr := strings.Split(decrypted, ":")
		if len(decrypted_arr) > 1 {
			method = decrypted_arr[0]
			password = strings.Join(decrypted_arr[1:], ":")
		} else {
			return nil, "", common.NewError("Unsupported shadowsocks")
		}
	}
	if strings.TrimSpace(method) == "" || password == "" {
		return nil, "", common.NewError("Invalid shadowsocks credentials")
	}
	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	ss := map[string]interface{}{
		"type":        "shadowsocks",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"method":      method,
		"password":    password,
	}
	v2ray_type := query.Get("type")
	if len(v2ray_type) > 0 {
		pl_arr := []string{}
		host_header := query.Get("host")
		if query.Get("security") == "tls" {
			pl_arr = append(pl_arr, "tls")
		}
		if v2ray_type == "quic" {
			pl_arr = append(pl_arr, "mode=quic")
		}
		if len(host_header) > 0 {
			pl_arr = append(pl_arr, "host="+host_header)
		}
		ss["plugin"] = "v2ray-plugin"
		ss["plugin_opts"] = strings.Join(pl_arr, ";")
	}
	plugin := query.Get("plugin")
	if len(plugin) > 0 {
		pl_arr := strings.Split(plugin, ";")
		if len(pl_arr) > 0 {
			ss["plugin"] = pl_arr[0]
			ss["plugin_opts"] = strings.Join(pl_arr[1:], ";")
		}
	}
	return &ss, tag, nil
}
func parseNaiveLink(u *url.URL, i int) (*map[string]interface{}, string, error) {
	var host, username, password string
	var port int
	switch u.Scheme {
	case "http2":
		decoded := uricodec.DecodeOrOriginal(u.Hostname())
		if idx := strings.LastIndex(decoded, "@"); idx > 0 {
			userInfo := decoded[:idx]
			hostPort := decoded[idx+1:]
			if idx2 := strings.Index(userInfo, ":"); idx2 > 0 {
				username = userInfo[:idx2]
				password = userInfo[idx2+1:]
				if password == "" {
					return nil, "", common.NewError("Invalid naive link credentials")
				}
			} else {
				return nil, "", common.NewError("Invalid naive link credentials")
			}
			endpoint, parseErr := url.Parse("naive+https://" + hostPort)
			if parseErr != nil {
				return nil, "", common.NewError("Invalid naive link endpoint")
			}
			host, port, parseErr = parseEndpoint(endpoint, 443)
			if parseErr != nil {
				return nil, "", parseErr
			}
		} else {
			return nil, "", common.NewError("Invalid naive link (http2)")
		}
	case "naive+https", "naive+quic":
		var endpointErr error
		host, port, endpointErr = parseEndpoint(u, 443)
		if endpointErr != nil {
			return nil, "", endpointErr
		}
		if u.User != nil {
			username = u.User.Username()
			password, _ = u.User.Password()
		}
	default:
		return nil, "", common.NewError("Unsupported naive scheme")
	}
	if strings.TrimSpace(username) == "" || password == "" {
		return nil, "", common.NewError("Invalid naive link credentials")
	}
	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	if tag == "" {
		tag = fmt.Sprintf("naive-%d", i)
	}
	naive := map[string]interface{}{
		"type":        "naive",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"username":    username,
		"password":    password,
		"tls":         map[string]interface{}{"enabled": true},
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, "", common.NewError("invalid naive query")
	}
	if peer := query.Get("peer"); peer != "" {
		if tls, ok := naive["tls"].(map[string]interface{}); ok {
			tls["server_name"] = peer
		}
	}
	if insecure := query.Get("insecure"); insecure == "1" || insecure == "true" {
		if tls, ok := naive["tls"].(map[string]interface{}); ok {
			tls["insecure"] = true
		}
	}
	if alpn := query.Get("alpn"); alpn != "" {
		if tls, ok := naive["tls"].(map[string]interface{}); ok {
			tls["alpn"] = strings.Split(alpn, ",")
		}
	}
	if u.Scheme == "naive+quic" {
		naive["quic"] = true
	}
	return &naive, tag, nil
}
