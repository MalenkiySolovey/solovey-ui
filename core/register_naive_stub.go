//go:build !with_naive_outbound

package core

import (
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/sagernet/sing-box/adapter/outbound"
)

func registerNaiveOutbound(registry *outbound.Registry) {
	// naive outbound is disabled when built without with_naive_outbound tag
	logger.Error("naive outbound is disabled when built without with_naive_outbound tag")
}
