//go:build !minimal

package api

import (
	_ "github.com/MalenkiySolovey/solovey-ui/components/import-xui"
	_ "github.com/MalenkiySolovey/solovey-ui/components/observability-extra"
	_ "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions"
	_ "github.com/MalenkiySolovey/solovey-ui/components/panel-update-ui"
	_ "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions"
	_ "github.com/MalenkiySolovey/solovey-ui/components/telegram"
)
