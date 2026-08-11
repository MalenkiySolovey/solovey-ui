package classifier

import (
	"slices"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

func TestClassifyHTTPProducesOnlySafeClasses(t *testing.T) {
	result := ClassifyHTTP(HTTPInput{
		Method:    "TRACE",
		Path:      "/wp-admin/?token=super-secret",
		UserAgent: "curl/8.0",
		Status:    404,
	})
	if result.Meta.PathClass != "scanner_path" || result.Meta.UAClass != "ua_scanner" || result.Meta.MethodClass != "unexpected" || result.Meta.StatusClass != "4xx" {
		t.Fatalf("unexpected classes: %#v", result.Meta)
	}
	for _, forbidden := range []string{"token", "secret", "wp-admin"} {
		if strings.Contains(strings.ToLower(strings.Join(result.Meta.DedupeParts(), " ")), forbidden) {
			t.Fatalf("safe metadata leaked %q: %#v", forbidden, result.Meta)
		}
	}
	for _, expected := range []domain.SignalKind{domain.SignalHTTPScannerPath, domain.SignalHTTPScannerUA, domain.SignalHTTPUnexpectedMethod} {
		if !slices.Contains(result.Signals, expected) {
			t.Fatalf("missing signal %s: %#v", expected, result.Signals)
		}
	}
}

func TestClassifyHTTPHandlesInvalidAndOverlongInput(t *testing.T) {
	invalid := ClassifyHTTP(HTTPInput{Path: string([]byte{0xff}), UserAgent: string([]byte{0xff}), Method: "GET"})
	if invalid.Meta.PathClass != "invalid_utf8" || invalid.Meta.UAClass != "invalid_utf8" {
		t.Fatalf("invalid UTF-8 classes: %#v", invalid.Meta)
	}
	overlong := ClassifyHTTP(HTTPInput{Path: "/" + strings.Repeat("a", 5000), UserAgent: strings.Repeat("b", 5000), Method: "GET"})
	if overlong.Meta.PathClass != "overlong_uri" || overlong.Meta.UAClass != "ua_overlong" {
		t.Fatalf("overlong classes: %#v", overlong.Meta)
	}
}
