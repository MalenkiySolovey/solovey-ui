//go:build linux && !minimal && zapretproof

package privilegedbroker

import (
	"context"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

// ServeZapretSockets is the isolated BPF proof adapter, not the systemd
// installed path. The socket transport and authentication are shared.
func (s *Server) ServeZapretSockets(ctx context.Context, scope planner.ZapretSocketScope, peer PeerIdentity,
	path *nftables.ZapretNativePath, observe func(context.Context) (planner.Facts, error), publish func(context.Context, string) error,
) (failure error) {
	if s == nil || path == nil || observe == nil || publish == nil || !path.ClaimSocketPublication(scope) {
		return errZapretSocket
	}
	var authority *authorityLease
	defer func() {
		failure = errors.Join(failure, path.Close())
		authority.Release()
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	expires, ok := path.Lifetime().Deadline()
	if !ok || !time.Now().Before(expires) {
		return errZapretSocket
	}
	grant := ZapretSocketGrant{Scope: scope, Generation: path.Generation(), PeerRevision: peer.Revision, Expires: expires}
	journal, ok := s.Journal.(*FileJournal)
	if !ok || journal == nil || journal.bootID != s.BootID || peer.BootID != s.BootID || journal.checkZapretPath(grant) != nil {
		return errZapretSocket
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(path.Lifetime(), cancel)
	defer stop()
	var err error
	authority, err = s.beginAuthority(ctx)
	if err != nil {
		return err
	}
	if _, ok := authority.Attestor.(ManifestAttestor); !ok || !authority.Revisioned || authority.Revision != peer.ManifestRevision {
		return errZapretSocket
	}
	ctx = authority.Context
	stopRetirement := context.AfterFunc(ctx, func() { _ = path.Close() })
	defer stopRetirement()
	gid, err := lookupBrokerGroup()
	if err != nil {
		return err
	}
	endpoint, err := bindZapretSocket(ctx, zapretSocketRoot, gid, grant.Generation)
	if err != nil {
		return err
	}
	return s.serveZapretSockets(ctx, endpoint, grant, func(ctx context.Context, expected ZapretSocketGrant, fd int, admit func() error) error {
		return path.WithSocket(ctx, observe, expected.Scope, expected.Generation, fd, admit)
	}, func(ctx context.Context, name string) error {
		if ctx.Err() != nil || !path.AdmissionReady(grant.Scope, grant.Generation) || journal.checkZapretPath(grant) != nil {
			return errZapretSocket
		}
		return publish(ctx, name)
	})
}
