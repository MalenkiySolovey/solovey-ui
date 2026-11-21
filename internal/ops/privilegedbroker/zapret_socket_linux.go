//go:build linux && !minimal && zapretproof

package privilegedbroker

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"golang.org/x/sys/unix"
	"golang.org/x/time/rate"
)

var errZapretSocket = errors.New("socket admission for Zapret denied")

// ZapretSocketGrant is supplied by the broker's trusted generation owner,
// never decoded from a Panel request. Generation identifies one live install
// of a plan, not a deterministic plan revision or reusable bearer token.
type ZapretSocketGrant struct {
	Scope        planner.ZapretSocketScope
	Generation   string
	PeerRevision string
	Expires      time.Time
}

// zapretSocketFence must hold the current-generation fence until admit returns.
// Before admit it proves the live allocations and enrolls this descriptor in
// the independent, persistent kernel guard. Enrollment must last until the
// socket dies, even if marking or the ACK fails or the broker exits. Revocation
// disables traffic for enrolled sockets, not just new admissions.
type zapretSocketFence func(ctx context.Context, grant ZapretSocketGrant, fd int, admit func() error) error

// ServeZapretSockets creates and publishes a root-owned socket after the
// committed generation is ready. The peer comes from the current manifest
// attestor and the generation supplies the grant expiry.
// publish runs before accept and must not dial the socket. observe is broker-local
// and supplies fresh Planner facts. Once publication is claimed, this call retires
// the generation on any exit, including failure before the listener is bound.
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
	// Rotation must revoke existing traffic, not just deny the next FD.
	// Retire even if publication or a client handler has not returned yet.
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

func (s *Server) serveZapretSockets(ctx context.Context, endpoint *standaloneSocket, grant ZapretSocketGrant, fence zapretSocketFence,
	publish func(context.Context, string) error,
) (failure error) {
	if endpoint == nil {
		return errZapretSocket
	}
	defer func() { failure = errors.Join(failure, endpoint.close()) }()
	if s == nil || fence == nil || publish == nil || !grant.Scope.Valid() || !digestPattern.MatchString(grant.Generation) ||
		!digestPattern.MatchString(grant.PeerRevision) || !time.Now().Before(grant.Expires) {
		return errZapretSocket
	}
	listener := endpoint.listener
	ctx, cancel := context.WithDeadline(ctx, grant.Expires)
	var workers sync.WaitGroup
	defer workers.Wait()
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := publish(ctx, endpoint.path); err != nil {
		return err
	}
	rateLimit := rate.NewLimiter(64, 16)
	active := make(chan struct{}, 4)
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if !rateLimit.Allow() {
			_ = conn.Close()
			continue
		}
		select {
		case active <- struct{}{}:
			workers.Go(func() {
				defer func() { <-active }()
				s.serveZapretSocket(ctx, conn, grant, fence)
			})
		default:
			_ = conn.Close()
		}
	}
}

func (s *Server) serveZapretSocket(ctx context.Context, conn *net.UnixConn, grant ZapretSocketGrant, fence zapretSocketFence) {
	defer conn.Close()
	started := time.Now()
	event := AuditEvent{Verb: auditZapretSocket, PeerRole: RolePanel, ResultClass: "denied_peer", Phase: AuditPhaseInitialAttestation}
	defer func() {
		event.DurationClass = durationClass(time.Since(started))
		s.emitAudit(event, event.ResultClass != "success")
	}()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	lease, err := s.beginAuthority(ctx)
	if err != nil {
		return
	}
	defer lease.Release()
	attestor, ok := lease.Attestor.(ManifestAttestor)
	if !ok || !lease.Revisioned {
		return
	}
	stop := context.AfterFunc(lease.Context, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(started.Add(2 * time.Second))
	peer, err := attestor.Attest(lease.Context, conn, RolePanel)
	if err != nil {
		event.PeerAttestation = string(peerAttestationClass(err))
		return
	}
	defer attestor.ClosePeer(peer)
	event.PeerRevision = peer.Revision
	if peer.BootID != s.BootID || peer.ManifestRevision != lease.Revision || peer.Revision != grant.PeerRevision ||
		peer.UID == 0 || peer.CapabilitiesOnly || peer.Supervisor != "systemd" || peer.CgroupPolicy != CgroupRequired {
		return
	}
	event.Phase = AuditPhaseRequestReceive
	writer, fd, err := readZapretSocket(conn)
	if err != nil {
		event.ResultClass = "denied_invalid_request"
		return
	}
	defer unix.Close(fd)
	if err := attestor.VerifyWriter(lease.Context, peer, writer); err != nil {
		event.PeerAttestation = string(peerAttestationClass(err))
		return
	}
	if err := checkZapretSocket(fd, peer, grant.Scope); err != nil {
		event.ResultClass = "denied_validation_failed"
		return
	}
	event.Phase = AuditPhaseDispatchGate
	event.ResultClass = "denied_stale_fence"
	used := false
	_ = fence(lease.Context, grant, fd, func() error {
		if used || lease.Context.Err() != nil || !time.Now().Before(grant.Expires) || !s.authorityCurrent(lease.Revision) {
			return errZapretSocket
		}
		used = true
		event.Phase = AuditPhaseFinalRecheck
		if err := attestor.Recheck(lease.Context, peer, RolePanel); err != nil {
			event.PeerAttestation = string(peerAttestationClass(err))
			return err
		}
		if err := checkZapretSocket(fd, peer, grant.Scope); err != nil {
			return err
		}
		s.authorityMu.Lock()
		defer s.authorityMu.Unlock()
		if s.retiring || s.authorityRevision != lease.Revision || lease.Context.Err() != nil || !time.Now().Before(grant.Expires) {
			return errZapretSocket
		}
		event.Phase = AuditPhaseDispatchGate
		event.ResultClass = "denied_execution_failed"
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_BINDTOIFINDEX, int(grant.Scope.Egress().IfIndex)); err != nil {
			return err
		}
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK, int(grant.Scope.Mark())); err != nil {
			return err
		}
		// The provider treats any one-byte response as success. A denial
		// cannot use an error byte; the connection must close without a reply.
		if n, err := conn.Write([]byte{1}); err != nil || n != 1 {
			return errZapretSocket
		}
		event.ResultClass = "success"
		return nil
	})
}

func readZapretSocket(conn *net.UnixConn) (WriterCredentials, int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return WriterCredentials{}, -1, err
	}
	var payload [2]byte
	oob := make([]byte, unix.CmsgSpace(unix.SizeofUcred)+unix.CmsgSpace(2*4))
	var n, oobn, flags int
	var recvErr error
	if err := raw.Read(func(fd uintptr) bool {
		n, oobn, flags, _, recvErr = unix.Recvmsg(int(fd), payload[:], oob, unix.MSG_CMSG_CLOEXEC)
		return !errors.Is(recvErr, unix.EAGAIN) && !errors.Is(recvErr, unix.EWOULDBLOCK)
	}); err != nil || recvErr != nil {
		return WriterCredentials{}, -1, errZapretSocket
	}
	return parseZapretControl(payload[:n], flags, oob[:oobn])
}

func parseZapretControl(payload []byte, flags int, oob []byte) (WriterCredentials, int, error) {
	bad := len(payload) != 1 || payload[0] != 1 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0
	var writer WriterCredentials
	fd := -1
	for len(oob) != 0 {
		header, data, rest, err := unix.ParseOneSocketControlMessage(oob)
		if err != nil {
			bad = true
			break
		}
		oob = rest
		message := &unix.SocketControlMessage{Header: header, Data: data}
		switch {
		case header.Level == unix.SOL_SOCKET && header.Type == unix.SCM_RIGHTS:
			rights, err := unix.ParseUnixRights(message)
			bad = bad || err != nil || len(rights) != 1 || fd != -1
			for _, right := range rights {
				if fd != -1 {
					_ = unix.Close(right)
				} else {
					fd = right
				}
			}
		case header.Level == unix.SOL_SOCKET && header.Type == unix.SCM_CREDENTIALS && len(data) == unix.SizeofUcred:
			cred, err := unix.ParseUnixCredentials(message)
			if err != nil || cred.Pid <= 1 || writer.PID != 0 {
				bad = true
				continue
			}
			writer = WriterCredentials{PID: int(cred.Pid), UID: cred.Uid, GID: cred.Gid}
		default:
			bad = true
		}
	}
	if bad || fd < 0 || writer.PID == 0 {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		return WriterCredentials{}, -1, errZapretSocket
	}
	return writer, fd, nil
}

func checkZapretSocket(fd int, peer PeerIdentity, scope planner.ZapretSocketScope) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFSOCK || stat.Uid != peer.UID || stat.Gid != peer.GID {
		return errZapretSocket
	}
	for _, opt := range []int{unix.SO_MARK, unix.SO_BINDTOIFINDEX, unix.SO_ACCEPTCONN} {
		if value, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, opt); err != nil || value != 0 {
			return errZapretSocket
		}
	}
	kind, typeErr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	protocol, protoErr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PROTOCOL)
	if typeErr != nil || protoErr != nil || !((kind == unix.SOCK_STREAM && protocol == unix.IPPROTO_TCP && scope.AllowsProtocol(intent.PacketProtocolTCP)) ||
		(kind == unix.SOCK_DGRAM && protocol == unix.IPPROTO_UDP && scope.AllowsProtocol(intent.PacketProtocolUDP))) {
		return errZapretSocket
	}
	address, err := unix.Getsockname(fd)
	if err != nil {
		return errZapretSocket
	}
	egress := scope.Egress()
	switch addr := address.(type) {
	case *unix.SockaddrInet4:
		if !egress.IPv4Gateway.IsValid() || addr.Port != 0 || addr.Addr != [4]byte{} {
			return errZapretSocket
		}
	case *unix.SockaddrInet6:
		if !egress.IPv6Gateway.IsValid() || addr.Port != 0 || addr.Addr != [16]byte{} || addr.ZoneId != 0 {
			return errZapretSocket
		}
	default:
		return errZapretSocket
	}
	if _, err := unix.Getpeername(fd); !errors.Is(err, unix.ENOTCONN) {
		return errZapretSocket
	}
	cookie, err := socketNetNSCookie(fd)
	if err != nil || cookie != egress.NamespaceCookie {
		return errZapretSocket
	}
	return nil
}

func socketNetNSCookie(fd int) (uint64, error) {
	cookie, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	if err != nil {
		return 0, err
	}
	if cookie == 0 {
		return 0, errZapretSocket
	}
	return cookie, nil
}
