package uri

import (
	"encoding/json"
	"fmt"
	"strings"
)

func socksLink(userConfig map[string]interface{}, addrs []map[string]interface{}) []string {
	var links []string
	for _, addr := range addrs {
		port, _ := addr["server_port"].(float64)
		links = append(links, fmt.Sprintf("socks5://%s:%s@%s:%d", userConfig["username"], userConfig["password"], mapString(addr, "server"), uint(port)))
	}
	return links
}
func httpLink(userConfig map[string]interface{}, addrs []map[string]interface{}) []string {
	var links []string
	protocol := "http"
	for _, addr := range addrs {
		if addr["tls"] != nil {
			protocol = "https"
		}
		port, _ := addr["server_port"].(float64)
		links = append(links, fmt.Sprintf("%s://%s:%s@%s:%d", protocol, userConfig["username"], userConfig["password"], mapString(addr, "server"), uint(port)))
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
	uriBase := fmt.Sprintf("ss://%s", toBase64([]byte(fmt.Sprintf("%s:%s", method, strings.Join(userPass, ":")))))
	var links []string
	for _, addr := range addrs {
		port, _ := addr["server_port"].(float64)
		links = append(links, fmt.Sprintf("%s@%s:%.0f#%s", uriBase, mapString(addr, "server"), port, mapString(addr, "remark")))
	}
	return links
}
func naiveLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {
	password, _ := userConfig["password"].(string)
	username, _ := userConfig["username"].(string)
	baseUri := "http2://"
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
		port, _ := addr["server_port"].(float64)
		uri := baseUri + toBase64([]byte(fmt.Sprintf("%s:%s@%s:%.0f", username, password, mapString(addr, "server"), port)))
		links = append(links, addParams(uri, params, mapString(addr, "remark")))
	}
	return links
}
func hysteriaLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {
	baseUri := "hysteria://"
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
		var outJson map[string]interface{}
		outRaw, _ := inbound["out_json"].(json.RawMessage)
		if err := json.Unmarshal(outRaw, &outJson); err != nil {
			return []string{} // Handle error
		}
		if mport, ok := outJson["server_ports"].([]interface{}); ok {
			mportList := make([]string, len(mport))
			for i, v := range mport {
				mportList[i], _ = v.(string)
			}
			params = append(params, LinkParam{"mport", strings.Join(mportList, ",")})
		}
		port, _ := addr["server_port"].(float64)
		uri := fmt.Sprintf("%s%s:%.0f", baseUri, mapString(addr, "server"), port)
		links = append(links, addParams(uri, params, mapString(addr, "remark")))
	}
	return links
}
func hysteria2Link(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {
	password, _ := userConfig["password"].(string)
	baseUri := fmt.Sprintf("%s%s@", "hysteria2://", password)
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
		var outJson map[string]interface{}
		outRaw, _ := inbound["out_json"].(json.RawMessage)
		if err := json.Unmarshal(outRaw, &outJson); err != nil {
			return []string{} // Handle error
		}
		if mport, ok := outJson["server_ports"].([]interface{}); ok {
			mportList := make([]string, len(mport))
			for i, v := range mport {
				mportList[i], _ = v.(string)
			}
			params = append(params, LinkParam{"mport", strings.Join(mportList, ",")})
		}
		port, _ := addr["server_port"].(float64)
		uri := fmt.Sprintf("%s%s:%.0f", baseUri, mapString(addr, "server"), port)
		links = append(links, addParams(uri, params, mapString(addr, "remark")))
	}
	return links
}
