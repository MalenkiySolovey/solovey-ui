package uri

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	uricodec "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func vmess(data string, i int) (*map[string]interface{}, string, error) {
	dataByte, err := uricodec.Decode(data)
	if err != nil {
		return nil, "", err
	}
	var dataJson map[string]interface{}
	err = json.Unmarshal(dataByte, &dataJson)
	if err != nil {
		return nil, "", err
	}
	transport := map[string]interface{}{}
	tp_net, _ := dataJson["net"].(string)
	tp_type, _ := dataJson["type"].(string)
	tp_host, _ := dataJson["host"].(string)
	tp_path, _ := dataJson["path"].(string)
	switch strings.ToLower(tp_net) {
	case "tcp", "":
		if tp_type == "http" {
			transport["type"] = tp_type
			if len(tp_host) > 0 {
				transport["host"] = strings.Split(tp_host, ",")
			}
			transport["path"] = tp_path
		}
	case "http", "h2":
		transport["type"] = "http"
		if len(tp_host) > 0 {
			transport["host"] = strings.Split(tp_host, ",")
		}
		transport["path"] = tp_path
	case "ws":
		transport["type"] = tp_net
		transport["path"] = tp_path
		transport["early_data_header_name"] = "Sec-WebSocket-Protocol"
		if len(tp_host) > 0 {
			transport["headers"] = map[string]interface{}{
				"Host": tp_host,
			}
		}
	case "quic":
		transport["type"] = tp_net
	case "grpc":
		transport["type"] = tp_net
		transport["service_name"] = tp_path
	case "httpupgrade":
		transport["type"] = tp_net
		transport["path"] = tp_path
		transport["host"] = tp_host
	default:
		return nil, "", common.NewError("Invalid vmess")
	}
	tls := map[string]interface{}{}
	vmess_tls, _ := dataJson["tls"].(string)
	if vmess_tls == "tls" {
		tls["enabled"] = true
		tls_sni, _ := dataJson["sni"].(string)
		tls_alpn, _ := dataJson["alpn"].(string)
		_, tls_insecure := dataJson["allowInsecure"]
		tls_fp, _ := dataJson["fp"].(string)
		if len(tls_sni) > 0 {
			tls["server_name"] = tls_sni
		}
		if len(tls_alpn) > 0 {
			tls["alpn"] = strings.Split(tls_alpn, ",")
		}
		if tls_insecure {
			tls["insecure"] = true
		}
		if len(tls_fp) > 0 {
			tls["utls"] = map[string]interface{}{
				"enabled":     true,
				"fingerprint": tls_fp,
			}
		}
	}
	tag, _ := dataJson["ps"].(string)
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, tag)
	}
	alter_id := 0
	if aid, ok := dataJson["aid"].(float64); ok {
		alter_id = int(aid)
	}
	vmess := map[string]interface{}{
		"type":        "vmess",
		"tag":         tag,
		"server":      dataJson["add"],
		"server_port": dataJson["port"],
		"uuid":        dataJson["id"],
		"security":    "auto",
		"alter_id":    alter_id,
		"tls":         tls,
		"transport":   transport,
	}
	return &vmess, tag, err
}
func vless(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, _ := url.ParseQuery(u.RawQuery)
	security := query.Get("security")
	host, portStr, _ := net.SplitHostPort(u.Host)
	port := 80
	if len(portStr) > 0 {
		port, _ = strconv.Atoi(portStr)
	} else {
		if security == "tls" || security == "reality" {
			port = 443
		}
	}
	tp_type := query.Get("type")
	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	vless := map[string]interface{}{
		"type":        "vless",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"uuid":        u.User.Username(),
		"flow":        query.Get("flow"),
		"tls":         getTls(security, &query),
		"transport":   getTransport(tp_type, &query),
	}
	return &vless, tag, nil
}
func trojan(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, _ := url.ParseQuery(u.RawQuery)
	security := query.Get("security")
	host, portStr, _ := net.SplitHostPort(u.Host)
	port := 80
	if len(portStr) > 0 {
		port, _ = strconv.Atoi(portStr)
	} else {
		if security == "tls" || security == "reality" {
			port = 443
		}
	}
	tp_type := query.Get("type")
	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	trojan := map[string]interface{}{
		"type":        "trojan",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"password":    u.User.Username(),
		"tls":         getTls(security, &query),
		"transport":   getTransport(tp_type, &query),
	}
	return &trojan, tag, nil
}
func hy(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, _ := url.ParseQuery(u.RawQuery)
	host, portStr, _ := net.SplitHostPort(u.Host)
	port := 443
	if len(portStr) > 0 {
		port, _ = strconv.Atoi(portStr)
	}
	security := query.Get("security")
	if len(security) == 0 {
		security = "tls"
	}
	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	hy := map[string]interface{}{
		"type":        "hysteria",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"obfs":        query.Get("obfsParam"),
		"auth_str":    query.Get("auth"),
		"tls":         getTls(security, &query),
	}
	down, _ := strconv.Atoi(query.Get("downmbps"))
	up, _ := strconv.Atoi(query.Get("upmbps"))
	recv_window_conn, _ := strconv.Atoi(query.Get("recv_window_conn"))
	recv_window, _ := strconv.Atoi(query.Get("recv_window"))
	if down > 0 {
		hy["down_mbps"] = down
	}
	if up > 0 {
		hy["up_mbps"] = up
	}
	if recv_window_conn > 0 {
		hy["recv_window_conn"] = recv_window_conn
	}
	if recv_window > 0 {
		hy["recv_window"] = recv_window
	}
	return &hy, tag, nil
}
func hy2(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, _ := url.ParseQuery(u.RawQuery)
	host, portStr, _ := net.SplitHostPort(u.Host)
	port := 443
	if len(portStr) > 0 {
		port, _ = strconv.Atoi(portStr)
	}
	security := query.Get("security")
	if len(security) == 0 {
		security = "tls"
	}
	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	hy2 := map[string]interface{}{
		"type":        "hysteria2",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"password":    u.User.Username(),
		"tls":         getTls(security, &query),
	}
	down, _ := strconv.Atoi(query.Get("downmbps"))
	up, _ := strconv.Atoi(query.Get("upmbps"))
	obfs := query.Get("obfs")
	mport := strings.ReplaceAll(query.Get("mport"), "-", ":")
	fastopen := query.Get("fastopen")
	if down > 0 {
		hy2["down_mbps"] = down
	}
	if up > 0 {
		hy2["up_mbps"] = up
	}
	if obfs == "salamander" {
		hy2["obfs"] = map[string]interface{}{
			"type":     "salamander",
			"password": query.Get("obfs-password"),
		}
	}
	if len(mport) > 0 {
		hy2["server_ports"] = strings.Split(mport, ",")
	}
	if fastopen == "1" || fastopen == "true" {
		hy2["fastopen"] = true
	}
	return &hy2, tag, nil
}
