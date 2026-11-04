//go:build !minimal

package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	subexternal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/external"
)

type SourceHealthCheck struct {
	Enabled  bool   `json:"enabled"`
	URL      string `json:"url,omitempty"`
	Interval string `json:"interval,omitempty"`
	Timeout  string `json:"timeout,omitempty"`
}

var ErrSourceHealthCheck = errors.New("SUBSCRIPTION_HEALTH_CHECK_INVALID")

func ParseSourceHealthCheck(raw json.RawMessage) (SourceHealthCheck, error) {
	var policy SourceHealthCheck
	if len(raw) == 0 {
		return policy, nil
	}
	if len(raw) > 16<<10 || jsonvalue.StrictObject(raw) != nil {
		return policy, ErrSourceHealthCheck
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return policy, ErrSourceHealthCheck
	}
	for key, value := range fields {
		if (key != "enabled" && key != "url" && key != "interval" && key != "timeout") || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return policy, ErrSourceHealthCheck
		}
	}
	if json.Unmarshal(raw, &policy) != nil {
		return policy, ErrSourceHealthCheck
	}
	policy.URL = strings.TrimSpace(policy.URL)
	if len(policy.URL) > 8192 {
		return policy, ErrSourceHealthCheck
	}
	if policy.Enabled {
		if policy.URL == "" {
			policy.URL = "https://www.gstatic.com/generate_204"
		}
		if policy.Interval == "" {
			policy.Interval = "10m"
		}
		if policy.Timeout == "" {
			policy.Timeout = "3s"
		}
	}
	for _, value := range []struct {
		raw      string
		min, max time.Duration
	}{
		{policy.Interval, time.Minute, 24 * time.Hour}, {policy.Timeout, 100 * time.Millisecond, 15 * time.Second},
	} {
		if value.raw == "" {
			continue
		}
		duration, err := time.ParseDuration(value.raw)
		if err != nil || duration < value.min || duration > value.max {
			return policy, ErrSourceHealthCheck
		}
	}
	return policy, nil
}

func PrepareSourceHealthCheck(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	policy, err := ParseSourceHealthCheck(raw)
	if err != nil {
		return nil, err
	}
	if policy.Enabled && subexternal.ValidateURL(policy.URL) != nil {
		return nil, ErrSourceHealthCheck
	}
	return json.Marshal(policy)
}

func projectSourceHealth(s *RemoteOutboundSubscription) error {
	policy, err := ParseSourceHealthCheck(s.HealthCheck)
	if err != nil {
		return err
	}
	s.HealthTargetID = ""
	if policy.Enabled {
		s.HealthTargetID = coreruntime.ProbeTargetID(policy.URL)
	}
	return nil
}
