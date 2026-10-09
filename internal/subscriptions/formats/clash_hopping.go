package formats

import (
	"errors"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func clashHysteriaPorts(value any) (string, error) {
	ports, err := canonical.HysteriaPorts(value)
	if err != nil {
		return "", err
	}
	for i, port := range ports {
		start, end, _ := strings.Cut(port, ":")
		if end == "" {
			end = "65535"
		}
		ports[i] = start
		if start != end {
			ports[i] += "-" + end
		}
	}
	return strings.Join(ports, ","), nil
}

func clashHysteriaInterval(value any) (int64, error) {
	if value == nil {
		return 0, nil
	}
	text, ok := value.(string)
	if !ok {
		return 0, errors.New("invalid Hysteria hop_interval")
	}
	if text == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(text)
	if err != nil || duration < 0 {
		return 0, errors.New("invalid Hysteria hop_interval")
	}
	if duration%time.Second != 0 {
		return 0, errors.New("Clash hop-interval requires whole seconds; use JSON to preserve the configured interval")
	}
	return int64(duration / time.Second), nil
}
