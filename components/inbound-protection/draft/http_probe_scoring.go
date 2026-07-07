//go:build inbound_protection_draft

package draft

import "strings"

type HTTPProbeSignal struct {
	Weight int
	Reason string
}

type HTTPProbeInput struct {
	Method    string
	Path      string
	UserAgent string
	Outcome   string
}

// ScoreHTTPProbe is draft-only groundwork for the future inbound-protection
// component. It deliberately lives outside fallback-html because the result is
// a panel-wide signal, not a property of one public site.
func ScoreHTTPProbe(input HTTPProbeInput) []HTTPProbeSignal {
	signals := []HTTPProbeSignal{}
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	path := strings.ToLower(strings.TrimSpace(input.Path))
	userAgent := strings.ToLower(strings.TrimSpace(input.UserAgent))
	outcome := strings.ToLower(strings.TrimSpace(input.Outcome))

	if method != "GET" && method != "HEAD" {
		signals = append(signals, HTTPProbeSignal{Weight: 3, Reason: "unexpected-method"})
	}
	if userAgent == "" {
		signals = append(signals, HTTPProbeSignal{Weight: 2, Reason: "empty-user-agent"})
	} else if suspiciousUserAgent(userAgent) {
		signals = append(signals, HTTPProbeSignal{Weight: 2, Reason: "scanner-user-agent"})
	}
	if suspiciousPath(path) {
		signals = append(signals, HTTPProbeSignal{Weight: 3, Reason: "scanner-path"})
	}
	switch outcome {
	case "reserved", "invalid-path":
		signals = append(signals, HTTPProbeSignal{Weight: 3, Reason: outcome})
	case "not-found", "fallback-404":
		signals = append(signals, HTTPProbeSignal{Weight: 2, Reason: "fallback"})
	case "rate-limited":
		signals = append(signals, HTTPProbeSignal{Weight: 2, Reason: "rate-limited"})
	}
	return signals
}

func suspiciousUserAgent(userAgent string) bool {
	for _, marker := range []string{
		"curl", "wget", "python-requests", "go-http-client", "masscan", "zgrab",
		"nmap", "nikto", "sqlmap", "acunetix", "nessus", "scan",
	} {
		if strings.Contains(userAgent, marker) {
			return true
		}
	}
	return false
}

func suspiciousPath(path string) bool {
	for _, marker := range []string{
		"/.env", "/wp-", "/wordpress", "/phpmyadmin", "/admin", "/cgi-bin",
		"/vendor/phpunit", "/actuator", "/server-status", "/geoserver",
		"/login.action", "/boaform", "/shell", "/xmlrpc.php",
	} {
		if strings.Contains(path, marker) {
			return true
		}
	}
	return false
}
