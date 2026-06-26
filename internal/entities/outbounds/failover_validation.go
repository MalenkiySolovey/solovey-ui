package outbounds

import (
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/ssrf"
	"gorm.io/gorm"
)

func validateFailoverGroup(db *gorm.DB, outbound model.Outbound) error {
	opts, err := parseFailoverOptions(outbound.Options)
	if err != nil {
		return err
	}
	if len(opts.Outbounds) == 0 {
		return common.NewError("failover group needs at least one member")
	}

	var rows []model.Outbound
	if err := db.Model(model.Outbound{}).Select("tag", "type").Find(&rows).Error; err != nil {
		return err
	}
	typeByTag := make(map[string]string, len(rows))
	for _, row := range rows {
		typeByTag[row.Tag] = row.Type
	}

	seen := make(map[string]struct{}, len(opts.Outbounds))
	for _, member := range opts.Outbounds {
		if member == outbound.Tag {
			return common.NewError("a failover group cannot reference itself")
		}
		if _, duplicate := seen[member]; duplicate {
			return common.NewErrorf("member %q is listed more than once", member)
		}
		seen[member] = struct{}{}
		memberType, exists := typeByTag[member]
		if !exists {
			return common.NewErrorf("failover member %q does not exist", member)
		}
		if memberType == "selector" || memberType == "urltest" || memberType == FailoverType {
			return common.NewErrorf("member %q is a group; failover members must be plain outbounds", member)
		}
	}

	if err := validateProbeTarget(opts.Failover.target()); err != nil {
		return err
	}
	if opts.Failover.Interval != "" {
		duration, err := time.ParseDuration(opts.Failover.Interval)
		if err != nil {
			return common.NewErrorf("invalid probe interval %q", opts.Failover.Interval)
		}
		if duration < MinInterval {
			return common.NewErrorf("probe interval must be >= %s", MinInterval)
		}
	}
	if opts.Failover.Hysteresis < 0 {
		return common.NewError("hysteresis must be >= 1")
	}
	return nil
}

func validateProbeTarget(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return common.NewErrorf("invalid probe target: %v", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return common.NewError("probe target must be an http(s) URL")
	}
	if parsed.Hostname() == "" {
		return common.NewError("probe target must include a host")
	}
	if parsed.User != nil {
		return common.NewError("probe target must not include userinfo")
	}
	if addr, err := netip.ParseAddr(parsed.Hostname()); err == nil && ssrf.IsInfrastructureAddr(addr) {
		return common.NewError("probe target host is not allowed")
	}
	return nil
}
