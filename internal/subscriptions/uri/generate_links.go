package uri

import (
	"encoding/base64"
	"fmt"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
	"net/url"
	"strings"
)

func socksLink(userConfig map[string]interface{}, addrs []map[string]interface{}) []string {
	var links []string
	for _, addr := range addrs {
		links = append(links, linkURL("socks5", url.UserPassword(mapString(userConfig, "username"), mapString(userConfig, "password")), addr, nil, mapString(addr, "share_name")))
	}
	return links
}
func httpLink(userConfig map[string]interface{}, addrs []map[string]interface{}) []string {
	var links []string
	for _, addr := range addrs {
		protocol := "http"
		if addr["tls"] != nil {
			protocol = "https"
		}
		links = append(links, linkURL(protocol, url.UserPassword(mapString(userConfig, "username"), mapString(userConfig, "password")), addr, nil, mapString(addr, "share_name")))
	}
	return links
}
func shadowsocksLink(
	userConfig map[string]map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {
	var userPass []string
	method, _ := inbound["method"].(string)
	if strings.HasPrefix(method, "2022") {
		inbPass, _ := inbound["password"].(string)
		userPass = append(userPass, inbPass)
	}
	var pass string
	if method == "2022-blake3-aes-128-gcm" {
		pass, _ = userConfig["shadowsocks16"]["password"].(string)
	} else {
		pass, _ = userConfig["shadowsocks"]["password"].(string)
	}
	userPass = append(userPass, pass)
	password := strings.Join(userPass, ":")
	userinfo := url.User(base64.RawURLEncoding.EncodeToString([]byte(method + ":" + password)))
	if strings.HasPrefix(method, "2022-") {
		userinfo = url.UserPassword(method, password)
	}
	outbound, _ := decodeOutboundProjection(inbound)
	plugin, opts, _ := canonical.ShadowsocksPlugin(outbound)
	var links []string
	for _, addr := range addrs {
		var params []LinkParam
		if plugin != "" {
			value := plugin
			if opts != "" {
				value += ";" + opts
			}
			params = append(params, LinkParam{"plugin", value})
		}
		link := linkURL("ss", userinfo, addr, params, mapString(addr, "remark"))
		if plugin != "" {
			parsed, err := url.Parse(link)
			if err == nil {
				parsed.Path = "/"
				link = parsed.String()
			}
		}
		links = append(links, link)
	}
	return links
}
func naiveLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {
	password, _ := userConfig["password"].(string)
	username, _ := userConfig["username"].(string)
	schemes, _ := naiveSchemes(inbound["network"])
	var links []string
	for _, addr := range addrs {
		var params []LinkParam
		params = append(params, LinkParam{"padding", "1"})
		if tls, ok := addr["tls"].(map[string]interface{}); ok {
			if sni, ok := tls["server_name"].(string); ok {
				params = append(params, LinkParam{"peer", sni})
			}
			if alpn, ok := tls["alpn"].([]interface{}); ok {
				alpnList := make([]string, len(alpn))
				for i, v := range alpn {
					alpnList[i], _ = v.(string)
				}
				params = append(params, LinkParam{"alpn", strings.Join(alpnList, ",")})
			}
			if insecure, ok := tls["insecure"].(bool); ok && insecure {
				params = append(params, LinkParam{"insecure", "1"})
			}
		}
		if tfo, ok := inbound["tcp_fast_open"].(bool); ok && tfo {
			params = append(params, LinkParam{"tfo", "1"})
		} else {
			params = append(params, LinkParam{"tfo", "0"})
		}
		for _, scheme := range schemes {
			if scheme == "http2" {
				port, _ := addr["server_port"].(float64)
				authority, _ := codec.Authority(mapString(addr, "server"), uint16(port))
				payload := base64.RawURLEncoding.EncodeToString([]byte(username + ":" + password + "@" + authority))
				links = append(links, addParams("http2://"+payload, params, mapString(addr, "remark")))
			} else {
				links = append(links, linkURL(scheme, url.UserPassword(username, password), addr, params, mapString(addr, "remark")))
			}
		}
	}
	return links
}
func hysteriaLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {
	var links []string
	for _, addr := range addrs {
		var params []LinkParam
		if upmbps, ok := inbound["up_mbps"].(float64); ok {
			params = append(params, LinkParam{"downmbps", fmt.Sprintf("%.0f", upmbps)})
		}
		if downmbps, ok := inbound["down_mbps"].(float64); ok {
			params = append(params, LinkParam{"upmbps", fmt.Sprintf("%.0f", downmbps)})
		}
		if auth, ok := userConfig["auth_str"].(string); ok {
			params = append(params, LinkParam{"auth", auth})
		}
		if tls, ok := addr["tls"].(map[string]interface{}); ok {
			getTlsParams(&params, tls, "insecure")
		}
		if obfs, ok := inbound["obfs"].(string); ok {
			params = append(params, LinkParam{"obfs", obfs})
		}
		if tfo, ok := inbound["tcp_fast_open"].(bool); ok && tfo {
			params = append(params, LinkParam{"fastopen", "1"})
		} else {
			params = append(params, LinkParam{"fastopen", "0"})
		}
		if mport := portHoppingParam(inbound); mport != "" {
			params = append(params, LinkParam{"mport", mport})
		}
		links = append(links, linkURL("hysteria", nil, addr, params, mapString(addr, "remark")))
	}
	return links
}
func hysteria2Link(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {
	password, _ := userConfig["password"].(string)
	var links []string
	for _, addr := range addrs {
		var params []LinkParam
		if upmbps, ok := inbound["up_mbps"].(float64); ok {
			params = append(params, LinkParam{"downmbps", fmt.Sprintf("%.0f", upmbps)})
		}
		if downmbps, ok := inbound["down_mbps"].(float64); ok {
			params = append(params, LinkParam{"upmbps", fmt.Sprintf("%.0f", downmbps)})
		}
		if tls, ok := addr["tls"].(map[string]interface{}); ok {
			getTlsParams(&params, tls, "insecure")
		}
		if obfs, ok := inbound["obfs"].(map[string]interface{}); ok {
			if obfsType, ok := obfs["type"].(string); ok {
				params = append(params, LinkParam{"obfs", obfsType})
			}
			if obfsPassword, ok := obfs["password"].(string); ok {
				params = append(params, LinkParam{"obfs-password", obfsPassword})
			}
		}
		if tfo, ok := inbound["tcp_fast_open"].(bool); ok && tfo {
			params = append(params, LinkParam{"fastopen", "1"})
		} else {
			params = append(params, LinkParam{"fastopen", "0"})
		}
		if mport := portHoppingParam(inbound); mport != "" {
			params = append(params, LinkParam{"mport", mport})
		}
		links = append(links, linkURL("hysteria2", url.User(password), addr, params, mapString(addr, "remark")))
	}
	return links
}
