package outbounds

import (
	"encoding/json"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

const (
	FailoverType       = "failover"
	DirectTag          = "direct"
	DefaultProbeTarget = "https://www.gstatic.com/generate_204"
	DefaultInterval    = 30 * time.Second
	MinInterval        = 5 * time.Second
	DefaultHysteresis  = 2
)

type failoverProbe struct {
	Enabled     *bool  `json:"enabled,omitempty"`
	ProbeTarget string `json:"probe_target,omitempty"`
	Interval    string `json:"interval,omitempty"`
	Hysteresis  int    `json:"hysteresis,omitempty"`
}

type failoverOptions struct {
	Outbounds                 []string      `json:"outbounds"`
	Default                   string        `json:"default,omitempty"`
	InterruptExistConnections *bool         `json:"interrupt_exist_connections,omitempty"`
	Failover                  failoverProbe `json:"failover"`
}

func parseFailoverOptions(options json.RawMessage) (failoverOptions, error) {
	var opts failoverOptions
	if len(options) == 0 {
		return opts, common.NewError("failover group has no options")
	}
	if err := json.Unmarshal(options, &opts); err != nil {
		return opts, err
	}
	return opts, nil
}

func (p failoverProbe) enabled() bool {
	return p.Enabled == nil || *p.Enabled
}

func (p failoverProbe) interval() time.Duration {
	if p.Interval == "" {
		return DefaultInterval
	}
	duration, err := time.ParseDuration(p.Interval)
	if err != nil || duration < MinInterval {
		return DefaultInterval
	}
	return duration
}

func (p failoverProbe) hysteresis() int {
	if p.Hysteresis < 1 {
		return DefaultHysteresis
	}
	return p.Hysteresis
}

func (p failoverProbe) target() string {
	if p.ProbeTarget == "" {
		return DefaultProbeTarget
	}
	return p.ProbeTarget
}

// FailoverGroup is the manager-facing view of a persisted failover outbound.
type FailoverGroup struct {
	Tag         string
	Members     []string
	ProbeTarget string
	Interval    time.Duration
	Hysteresis  int
	Enabled     bool
}

// LoadFailoverGroups skips malformed legacy rows so one bad group cannot stop
// health management for all valid groups.
func LoadFailoverGroups(db *gorm.DB) ([]FailoverGroup, error) {
	var rows []model.Outbound
	if err := db.Model(model.Outbound{}).Where("type = ?", FailoverType).Find(&rows).Error; err != nil {
		return nil, err
	}
	groups := make([]FailoverGroup, 0, len(rows))
	for _, row := range rows {
		opts, err := parseFailoverOptions(row.Options)
		if err != nil || len(opts.Outbounds) == 0 {
			continue
		}
		groups = append(groups, FailoverGroup{
			Tag:         row.Tag,
			Members:     append([]string(nil), opts.Outbounds...),
			ProbeTarget: opts.Failover.target(),
			Interval:    opts.Failover.interval(),
			Hysteresis:  opts.Failover.hysteresis(),
			Enabled:     opts.Failover.enabled(),
		})
	}
	return groups, nil
}

// DirectFallbackTag returns a deterministic direct outbound tag, preferring
// the conventional seeded tag.
func DirectFallbackTag(db *gorm.DB) string {
	var tags []string
	if err := db.Model(model.Outbound{}).Where("type = ?", "direct").Order("tag").Pluck("tag", &tags).Error; err != nil {
		return ""
	}
	for _, tag := range tags {
		if tag == DirectTag {
			return DirectTag
		}
	}
	if len(tags) == 0 {
		return ""
	}
	return tags[0]
}
