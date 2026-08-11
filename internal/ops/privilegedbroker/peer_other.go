//go:build !linux

package privilegedbroker

import (
	"context"
	"errors"
	"net"
)

type ManifestAttestor struct{ Manifest Manifest }

func (ManifestAttestor) Attest(context.Context, *net.UnixConn, Role) (PeerIdentity, error) {
	return PeerIdentity{}, errors.New("privileged broker peer attestation requires Linux")
}
func (ManifestAttestor) Recheck(context.Context, PeerIdentity, Role) error {
	return errors.New("privileged broker peer attestation requires Linux")
}

func validateOwnedByRoot(string) error { return nil }
