//go:build !linux

package privilegedbroker

import (
	"context"
	"errors"
)

// ProcdInspector exists for composition compatibility; procd attestation is
// rejected by the non-Linux ManifestAttestor implementation.
type ProcdInspector struct{}

func NewProcdInspector() *ProcdInspector { return &ProcdInspector{} }

func (*ProcdInspector) Inspect(context.Context, ClientManifest) (ProcdInstanceEvidence, error) {
	return ProcdInstanceEvidence{}, errors.New("procd service evidence is supported only on Linux")
}
