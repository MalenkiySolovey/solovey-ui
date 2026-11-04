package external

import (
	"errors"
	"net/http"
	"strings"

	"golang.org/x/net/http/httpguts"
)

type sourceHeaderNamesKey struct{}

var ErrFetchHeadersInvalid = errors.New("SUBSCRIPTION_FETCH_HEADERS_INVALID")

// Source credentials may extend the request but cannot replace transport or
// message framing authority. User-Agent has its own existing source setting.
func ValidateFetchHeaders(headers http.Header) error {
	if len(headers) > 64 {
		return ErrFetchHeadersInvalid
	}
	total := 0
	seen := make(map[string]bool, len(headers))
	for name, values := range headers {
		key := http.CanonicalHeaderKey(name)
		if !httpguts.ValidHeaderFieldName(name) || len(name) > 256 || seen[key] || len(values) == 0 || len(values) > 16 {
			return ErrFetchHeadersInvalid
		}
		seen[key] = true
		switch key {
		case "Host", "Content-Length", "Transfer-Encoding", "Connection", "Trailer", "Te", "Upgrade", "User-Agent":
			return ErrFetchHeadersInvalid
		}
		if strings.HasPrefix(key, "Proxy-") || strings.HasPrefix(key, ":") {
			return ErrFetchHeadersInvalid
		}
		total += len(name)
		for _, value := range values {
			total += len(value)
			if !httpguts.ValidHeaderFieldValue(value) || total > 16<<10 {
				return ErrFetchHeadersInvalid
			}
		}
	}
	return nil
}
