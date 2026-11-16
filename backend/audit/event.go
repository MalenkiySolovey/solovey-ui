// Package audit owns Panel audit record values without storage or queue authority.
package audit

import hostaudit "github.com/MalenkiySolovey/solovey-host/auditrecord"

const (
	SeverityInfo      = "info"
	SeverityWarn      = "warn"
	ActorMaxBytes     = 256
	EventMaxBytes     = 128
	ResourceMaxBytes  = 128
	SeverityMaxBytes  = 16
	IPMaxBytes        = 64
	UserAgentMaxBytes = 512
	DetailsMaxBytes   = 16 * 1024
)

type Event = hostaudit.Event
