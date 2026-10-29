package external

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

// ProbeWithDialContext uses the fetch owner's destination/redirect boundary,
// without source credentials or a response-body download. Dial authority stays
// with the leased runtime outbound supplied by the canonical health owner.
func ProbeWithDialContext(ctx context.Context, target string, dial func(context.Context, string, string) (net.Conn, error)) (uint16, error) {
	if dial == nil {
		return 0, common.NewError("outbound probe is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ValidateURLContext(ctx, target); err != nil {
		return 0, err
	}
	client := newExternalHTTPClient()
	client.Transport.(*http.Transport).DialContext = validatedExternalDial(dial)
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, err
	}
	started := time.Now()
	response, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return 0, common.NewError("outbound probe status is unsuccessful")
	}
	return uint16(min(max(time.Since(started).Milliseconds(), 1), 65535)), nil
}
