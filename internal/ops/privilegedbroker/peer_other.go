//go:build !linux

package privilegedbroker

import (
	"context"
	"errors"
	"net"
)

type ManifestAttestor struct {
	Manifest Manifest
}

func NewManifestAttestor(manifest Manifest) (ManifestAttestor, error) {
	if _, err := manifest.SupervisorProof(); err != nil {
		return ManifestAttestor{}, err
	}
	return ManifestAttestor{Manifest: manifest}, nil
}

func (ManifestAttestor) Attest(context.Context, *net.UnixConn, Role) (PeerIdentity, error) {
	return PeerIdentity{}, attestationFailure(PeerAttestationInternalFailure, errors.New("privileged broker peer attestation requires Linux"))
}
func (ManifestAttestor) Recheck(context.Context, PeerIdentity, Role) error {
	return attestationFailure(PeerAttestationInternalFailure, errors.New("privileged broker peer attestation requires Linux"))
}

func validateOwnedByRoot(string) error { return nil }
