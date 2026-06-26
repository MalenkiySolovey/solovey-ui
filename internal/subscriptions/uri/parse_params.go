package uri

import (
	"net/url"
	"strings"
)

func getTransport(tp_type string, q *url.Values) map[string]interface{} {
	transport := map[string]interface{}{}
	tp_host := q.Get("host")
	tp_path := q.Get("path")
	switch strings.ToLower(tp_type) {
	case "tcp", "":
		if q.Get("headerType") == "http" {
			transport["type"] = "http"
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
		transport["type"] = "ws"
		transport["path"] = tp_path
		if len(tp_host) > 0 {
			transport["headers"] = map[string]interface{}{
				"Host": tp_host,
			}
		}
	case "quic":
		transport["type"] = "quic"
	case "grpc":
		transport["type"] = "grpc"
		transport["service_name"] = q.Get("serviceName")
	case "httpupgrade":
		transport["type"] = "httpupgrade"
		transport["path"] = tp_path
		transport["host"] = tp_host
	}
	return transport
}
func getTls(security string, q *url.Values) map[string]interface{} {
	tls := map[string]interface{}{}
	tls_fp := q.Get("fp")
	tls_sni := q.Get("sni")
	tls_allow_insecure := q.Get("allowInsecure")
	tls_insecure := q.Get("insecure")
	tls_alpn := q.Get("alpn")
	tls_ech := q.Get("ech")
	disable_sni := q.Get("disable_sni")
	switch security {
	case "tls":
		tls["enabled"] = true
	case "reality":
		tls["enabled"] = true
		tls["reality"] = map[string]interface{}{
			"enabled":    true,
			"public_key": q.Get("pbk"),
			"short_id":   q.Get("sid"),
		}
	}
	if len(tls_sni) > 0 {
		tls["server_name"] = tls_sni
	}
	if len(tls_alpn) > 0 {
		tls["alpn"] = strings.Split(tls_alpn, ",")
	}
	if tls_insecure == "1" || tls_insecure == "true" || tls_allow_insecure == "1" || tls_allow_insecure == "true" {
		tls["insecure"] = true
	}
	if len(tls_fp) > 0 {
		tls["utls"] = map[string]interface{}{
			"enabled":     true,
			"fingerprint": tls_fp,
		}
	}
	if len(tls_ech) > 0 {
		tls["ech"] = map[string]interface{}{
			"enabled": true,
			"config": []string{
				tls_ech,
			},
		}
	}
	if disable_sni == "1" || disable_sni == "true" {
		tls["disable_sni"] = true
	}
	return tls
}
