package api

import (
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/gin-gonic/gin"
)

// getRemoteIp returns the client IP, walking the X-Forwarded-For chain from the
// transport peer outward and returning the first hop that is not in the
// configured list of trusted proxies. Without trusted proxies it always
// returns the transport peer.
func getRemoteIp(c *gin.Context) string {
	remoteIP := canonicalClientIP(splitRemoteIP(c.Request.RemoteAddr))
	if !isTrustedProxy(remoteIP) {
		return remoteIP
	}
	value := c.GetHeader("X-Forwarded-For")
	if value == "" {
		return remoteIP
	}
	parts := strings.Split(value, ",")
	// Walk right-to-left: strip trusted proxies.
	for i := len(parts) - 1; i >= 0; i-- {
		hop := canonicalClientIP(strings.TrimSpace(parts[i]))
		if hop == "" {
			continue
		}
		if !isTrustedProxy(hop) {
			return hop
		}
	}
	return remoteIP
}

func splitRemoteIP(addr string) string {
	ip, _, err := net.SplitHostPort(addr)
	if err != nil {
		return strings.Trim(addr, "[]")
	}
	return strings.Trim(ip, "[]")
}

func canonicalClientIP(value string) string {
	value = strings.TrimSpace(strings.Trim(value, "[]"))
	if value == "" || strings.Contains(value, "%") {
		return ""
	}
	addr, err := netip.ParseAddr(value)
	if err != nil || addr.Zone() != "" {
		return ""
	}
	return addr.Unmap().String()
}

// RequestIsHTTPS reports whether the request arrived over HTTPS, trusting
// X-Forwarded-Proto only when the peer is a configured trusted proxy. Exported
// so the security-headers middleware can reuse this gated check for its HSTS
// decision (a spoofed X-Forwarded-Proto from an untrusted client must not
// trigger HSTS).
func RequestIsHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return isTrustedProxy(canonicalClientIP(splitRemoteIP(c.Request.RemoteAddr))) && strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

var (
	trustedProxiesMu     sync.Mutex
	trustedProxiesRaw    string
	trustedProxiesParsed []netip.Prefix
)

func parseTrustedProxies() []netip.Prefix {
	raw := os.Getenv("SUI_TRUSTED_PROXIES")
	trustedProxiesMu.Lock()
	defer trustedProxiesMu.Unlock()
	if raw == trustedProxiesRaw {
		return trustedProxiesParsed
	}
	trustedProxiesRaw = raw
	if raw == "" {
		trustedProxiesParsed = nil
		return nil
	}
	var parsed []netip.Prefix
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(item); err == nil {
			parsed = append(parsed, prefix)
			continue
		}
		if itemAddr, err := netip.ParseAddr(item); err == nil {
			itemAddr = itemAddr.Unmap()
			parsed = append(parsed, netip.PrefixFrom(itemAddr, itemAddr.BitLen()))
			continue
		}
		logger.Warningf("invalid SUI_TRUSTED_PROXIES entry: %q", item)
	}
	trustedProxiesParsed = parsed
	return parsed
}

func isTrustedProxy(remoteIP string) bool {
	prefixes := parseTrustedProxies()
	if len(prefixes) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(canonicalClientIP(remoteIP))
	if err != nil {
		return false
	}
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
