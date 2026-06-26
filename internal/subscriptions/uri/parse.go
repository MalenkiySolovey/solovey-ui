package uri

import (
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"net/url"
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
