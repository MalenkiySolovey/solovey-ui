package codec

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"unicode"
)

// NormalizeHost encodes only the endpoint already selected by its owner. A
// server field has no port; raw IPv6 and one matching bracket pair are accepted.
func NormalizeHost(host string) (string, error) {
	if host == "" || strings.TrimSpace(host) != host || strings.IndexFunc(host, unicode.IsSpace) >= 0 {
		return "", errors.New("invalid subscription server")
	}
	bracketed := strings.HasPrefix(host, "[") || strings.HasSuffix(host, "]")
	if bracketed {
		if !strings.HasPrefix(host, "[") || !strings.HasSuffix(host, "]") {
			return "", errors.New("invalid subscription server brackets")
		}
		host = host[1 : len(host)-1]
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if bracketed && !ip.Is6() {
			return "", errors.New("subscription server brackets require IPv6")
		}
		return ip.String(), nil
	}
	if bracketed || strings.ContainsAny(host, "[]:@/?#\\%") || strings.IndexFunc(host, unicode.IsControl) >= 0 {
		return "", errors.New("invalid subscription server")
	}
	return host, nil
}

func Authority(host string, port uint16) (string, error) {
	host, err := NormalizeHost(host)
	if err != nil || port == 0 {
		return "", errors.New("invalid subscription endpoint")
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}
