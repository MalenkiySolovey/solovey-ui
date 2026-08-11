//go:build !minimal

// Package schedule owns Telegram component schedule parsing rules.
package schedule

import (
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/robfig/cron/v3"
)

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func Parse(spec string) (cron.Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	schedule, err := parser.Parse(spec)
	if err != nil {
		return nil, err
	}
	first := schedule.Next(time.Unix(0, 0))
	second := schedule.Next(first)
	if !second.IsZero() && second.Sub(first) < time.Minute {
		return nil, common.NewError("telegram cron step must be at least 1 minute")
	}
	return schedule, nil
}
