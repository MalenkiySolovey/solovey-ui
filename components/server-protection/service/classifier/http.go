package classifier

import (
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

type HTTPInput struct {
	Method       string
	Path         string
	UserAgent    string
	Status       int
	RateLimited  bool
	ReservedPath bool
}

type HTTPResult struct {
	Signals []domain.SignalKind
	Meta    domain.SafeMeta
}

func ClassifyHTTP(input HTTPInput) HTTPResult {
	meta := domain.SafeMeta{
		PathClass:               publicsurface.ClassifyPath(input.Path, input.ReservedPath),
		UAClass:                 publicsurface.ClassifyUserAgent(input.UserAgent),
		MethodClass:             publicsurface.ClassifyMethod(input.Method),
		StatusClass:             publicsurface.ClassifyStatus(input.Status),
		ClassifierPolicyVersion: domain.ClassifierPolicyVersion,
	}
	signals := make([]domain.SignalKind, 0, 4)
	if isScannerPathClass(meta.PathClass) {
		signals = append(signals, domain.SignalHTTPScannerPath)
	}
	switch meta.UAClass {
	case "ua_empty":
		signals = append(signals, domain.SignalHTTPEmptyUA)
	case "ua_scanner":
		signals = append(signals, domain.SignalHTTPScannerUA)
	}
	if meta.MethodClass == "unexpected" {
		signals = append(signals, domain.SignalHTTPUnexpectedMethod)
	}
	if input.RateLimited {
		signals = append(signals, domain.SignalRateLimited)
	}
	return HTTPResult{Signals: signals, Meta: meta}
}

func isScannerPathClass(value string) bool {
	return value == "scanner_path" || strings.HasPrefix(value, "scanner_")
}
