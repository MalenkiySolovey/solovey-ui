//go:build !minimal

// Package schedule retains the Panel settings adapter import path.
package schedule

import (
	canonical "github.com/MalenkiySolovey/solovey-component-telegram/settings"
	"github.com/robfig/cron/v3"
)

func Parse(spec string) (cron.Schedule, error) { return canonical.ParseSchedule(spec) }
