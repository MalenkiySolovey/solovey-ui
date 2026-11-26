//go:build !linux && !minimal

package blockcheck

import (
	"context"

	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"github.com/MalenkiySolovey/solovey-host/hostref"
)

func hostBaselineAvailable(context.Context) bool { return false }

func openHostBaseline(context.Context, hostref.Ref, feature.PathSelection) (feature.Path, error) {
	return feature.Path{}, feature.ErrBaselineUnobservable
}
