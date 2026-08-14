// Package brokerplugin contributes the component's existing semantic engines
// to the neutral production privileged broker composition seam.
package brokerplugin

import (
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func init() {
	broker.RegisterHandlerContributor("server-protection", func(registry *broker.Registry) error {
		root, err := protectionhelper.NewManagedRoot(protectionruntime.Installed().HelperManagedRoot)
		if err != nil {
			return err
		}
		return protectionhelper.RegisterBrokerHandlers(registry, root)
	})
}
