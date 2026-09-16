package app

import (
	"context"
	"errors"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

// resetDatabaseResourceOwners refreshes the app-owned registration bundle
// before Restore can accept the new database. The same hook rebinds the exact
// fallback after a rejected restore, without replaying import normalization.
func (a *APP) resetDatabaseResourceOwners(ctx context.Context) error {
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	a.stopResourceRegistrations()
	if a.configService == nil {
		return errors.New("resource owners require initialized config service")
	}
	control := a.configService.CoreInboundControl()
	if control == nil {
		return errors.New("resource owners require current inbound database")
	}
	if _, err := control.ListSnapshots(ctx, hostresources.MaxResourceFacts+1); err != nil {
		return errors.Join(errors.New("bind current inbound resource owner"), err)
	}
	return a.registerResources()
}

func (a *APP) stopDatabaseHookRegistrations() {
	if a.stopDatabaseHooks != nil {
		a.stopDatabaseHooks()
		a.stopDatabaseHooks = nil
	}
}
