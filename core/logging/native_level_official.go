//go:build !shtorm_extended

package logging

import "github.com/sagernet/sing-box/log"

func panelLevel(level log.Level) log.Level { return level }
