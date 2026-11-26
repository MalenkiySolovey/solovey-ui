//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"golang.org/x/sys/unix"
)

// One installed batch shares one engine, owned table, and retained deny.
// Its members receive distinct, never-reused marks and admission endpoints.
type installedZapret struct {
	server  *Server
	ctx     context.Context
	journal *FileJournal
	resolve ResolveInstalledZapretStrategies
	deny    zapretDenyIdentity
	active  *installedZapretPath // accessed under the broker mutation fence
}

type installedZapretPath struct {
	inputDigest string
	grant       zapretactivation.BatchGrant
	queue       uint16
	enginePID   int
	peer        string
	uid         uint32
	ctx         context.Context
	release     context.CancelFunc
	done        chan struct{}
	stopErr     error // published by closing done
	mu          sync.Mutex
	verify      func(context.Context) error
	trace       ZapretNativeTrace
}

var errZapretCustomBlobsDenied = errors.New("the installed Zapret profile does not allow custom QUIC blobs")

func RegisterInstalledZapret(ctx context.Context, server *Server, transport TransportMode, listeners *ListenerSet, resolve ResolveInstalledZapretStrategies) error {
	if transport != SystemdActivated {
		return nil
	}
	journal, ok := server.Journal.(*FileJournal)
	if !ok || listeners == nil || resolve == nil {
		return errors.New("installed Zapret requires the broker file journal and socket activation")
	}
	owner := &installedZapret{ctx: ctx, server: server, journal: journal, resolve: resolve, deny: listeners.deny}
	if err := server.Registry.Register(VerbZapretValidate, Definition{Role: RolePanel, LocalPathAuthority: true, Handler: owner.validate}); err != nil {
		return err
	}
	if err := server.Registry.Register(VerbZapretAcquire, Definition{Role: RolePanel, LocalPathAuthority: true, Handler: owner.acquire}); err != nil {
		return err
	}
	if err := server.Registry.Register(VerbZapretObserve, Definition{Role: RolePanel, LocalPathAuthority: true, Handler: owner.observe}); err != nil {
		return err
	}
	return server.Registry.Register(VerbZapretRelease, Definition{Role: RolePanel, LocalPathAuthority: true, Handler: owner.retire})
}

func (z *installedZapret) observe(ctx context.Context, call Request, peer PeerIdentity) (any, error) {
	var expected zapretactivation.BatchIdentity
	if DecodePayload(call.Payload, &expected) != nil || !digestPattern.MatchString(expected.BatchID) {
		return nil, Failure(CodeValidation, "invalid Zapret observation generation")
	}
	path := z.active
	if path == nil || path.peer != peer.Revision || path.uid != peer.UID || path.grant.BatchID != expected.BatchID ||
		path.ctx.Err() != nil {
		return nil, Failure(CodeFence, "Zapret generation is not active")
	}
	if err := path.verify(ctx); err != nil {
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return nil, Failure(CodeDeadline, "Zapret generation could not be verified before the observation deadline")
		}
		return nil, Failure(CodeFence, "Zapret generation's live packet path could not be verified")
	}
	if path.ctx.Err() != nil {
		return nil, Failure(CodeFence, "Zapret generation retired during observation")
	}
	result := make([]zapretactivation.AppliedBinding, 0, len(path.grant.Bindings))
	for i, g := range path.grant.Bindings {
		binding := zapretactivation.AppliedBinding{Request: g.Request, StrategyRevision: g.StrategyRevision, Queue: path.queue}
		if path.trace != nil {
			binding.NativeMatches = &zapretactivation.NativeMatches{Profiles: path.trace.MatchedProfiles(i)}
		}
		result = append(result, binding)
	}
	return result, nil
}

func (z *installedZapret) validate(ctx context.Context, call Request, peer PeerIdentity) (any, error) {
	var input zapretactivation.Validation
	if DecodePayload(call.Payload, &input) != nil || !validZapretTargets(input.Bindings) ||
		peer.UID == 0 || peer.Supervisor != "systemd" || peer.CgroupPolicy != CgroupRequired || !peer.hasLiveness {
		return nil, Failure(CodeValidation, "invalid installed Zapret binding or Panel identity")
	}
	if err := checkZapretDescriptorBudget(len(input.Bindings), input.TUN != nil); err != nil {
		return nil, Failure(CodeValidation, err.Error())
	}
	if path := z.activeForPeer(peer); path != nil {
		// Reject a replacement that cannot fit even without its arguments,
		// before profile and rule validation allocate into the same cgroup.
		if err := checkZapretMemoryBudget(ctx, nil, path.enginePID); err != nil {
			return nil, Failure(CodeValidation, err.Error())
		}
	}
	profile, selected, err := z.selectStrategies(ctx, input)
	if err == nil {
		err = profile.verifyDialBindings(input.Bindings, 0)
	}
	if err == nil {
		err = profile.verifyTUN(ctx, input.TUN)
	}
	if err == nil {
		err = z.checkBatchBudget(ctx, input, peer, profile, selected)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "installed Zapret profile validation:", err)
		if errors.Is(err, errZapretCustomBlobsDenied) || errors.Is(err, errZapretDialBinding) || errors.Is(err, errZapretOuterScope) ||
			errors.Is(err, errZapretReplyAsset) || errors.Is(err, errZapretReplyHook) || errors.Is(err, errZapretDNSCoverage) || zapretCapacityFailure(err) {
			return nil, Failure(CodeValidation, err.Error())
		}
		return nil, Failure(CodeValidation, "installed Zapret does not authorize the selected binding or Strategy")
	}
	return nil, nil
}

func (z *installedZapret) acquire(ctx context.Context, call Request, peer PeerIdentity) (any, error) {
	var target zapretactivation.BatchRequest
	if err := DecodePayload(call.Payload, &target); err != nil || !digestPattern.MatchString(target.BatchID) ||
		!validZapretTargets(target.Bindings) ||
		peer.UID == 0 || peer.Supervisor != "systemd" || peer.CgroupPolicy != CgroupRequired || !peer.hasLiveness {
		return nil, Failure(CodeValidation, "invalid installed Zapret binding or Panel identity")
	}
	if existing := z.active; existing != nil && existing.ctx.Err() == nil {
		if existing.peer == peer.Revision && existing.grant.BatchID == target.BatchID && existing.inputDigest == Digest(call.Payload) {
			return existing.grant, nil
		}
		return nil, Failure(CodeRecoveryRequired, "the previous Zapret generation must be retired")
	}
	if z.active != nil {
		select {
		case <-z.active.done:
			if z.active.stopErr != nil {
				return nil, Failure(CodeRecoveryRequired, "the previous Zapret engine did not stop")
			}
			z.active = nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	path, err := z.prepare(ctx, target, peer)
	if err != nil {
		// Only fixed, owner-classified startup errors are returned to Panel.
		fmt.Fprintln(os.Stderr, "installed Zapret activation:", err)
		if errors.Is(err, errZapretReplyAsset) || errors.Is(err, errZapretReplyHook) || errors.Is(err, errZapretDNSCoverage) || zapretCapacityFailure(err) {
			return nil, Failure(CodeExecution, err.Error())
		}
		return nil, Failure(CodeExecution, "Zapret packet path not ready; see privileged broker journal")
	}
	path.inputDigest = Digest(call.Payload)
	z.active = path
	return path.grant, nil
}

func validZapretTargets(targets []zapretactivation.Request) bool {
	if len(targets) == 0 {
		return false
	}
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		if !safeIdentifier(target.OutboundRef) || !safeIdentifier(target.StrategyRef) ||
			!digestPattern.MatchString(target.OutboundRevision) || seen[target.OutboundRef] || !target.Dial.Valid() || !target.Outer.Valid() {
			return false
		}
		seen[target.OutboundRef] = true
	}
	return true
}

func (z *installedZapret) retire(ctx context.Context, call Request, peer PeerIdentity) (any, error) {
	var expected zapretactivation.Retirement
	if DecodePayload(call.Payload, &expected) != nil || !digestPattern.MatchString(expected.BatchID) {
		return nil, Failure(CodeValidation, "invalid Zapret retirement generation")
	}
	if z.active != nil {
		if z.active.grant.BatchID != expected.BatchID || z.active.uid != peer.UID ||
			z.active.ctx.Err() == nil && z.active.peer != peer.Revision {
			return nil, Failure(CodeFence, "Zapret retirement generation changed")
		}
		z.active.release()
		select {
		case <-z.active.done:
			if z.active.stopErr != nil {
				return nil, Failure(CodeRecoveryRequired, "the previous Zapret engine did not stop")
			}
			z.active = nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if z.deny.port != 0 {
		// A stopped broker may have left a descriptor in a still-running
		// Core. Never remove the retained ingress filter while it exists.
		if _, err := os.Lstat("/sys/class/net/" + zapretactivation.TUNInterface); !errors.Is(err, os.ErrNotExist) {
			return nil, Failure(CodeRecoveryRequired, "Zapret TUN deny remains until the old device is gone")
		}
		// Successful Core.Stop normally drains its tracked connections. If a
		// descriptor or a kernel retransmission survives, retain the deny
		// even when the current generation, broker or engine has stopped.
		if err := proveZapretDrain(ctx, peer); err != nil {
			fmt.Fprintln(os.Stderr, "installed Zapret deny retained:", err)
			return nil, Failure(CodeRecoveryRequired, "Zapret deny remains until old sockets are drained")
		}
		if err := removeZapretDenyFromSystemd(ctx, z.deny); err != nil {
			return nil, Failure(CodeRecoveryRequired, "Zapret deny retirement could not be confirmed")
		}
		z.deny = zapretDenyIdentity{}
	}
	return nil, nil
}

func (z *installedZapret) selectStrategies(ctx context.Context, input zapretactivation.Validation) (installedZapretProfile, InstalledZapretStrategies, error) {
	profile, err := readInstalledZapretProfile()
	if err != nil {
		return profile, InstalledZapretStrategies{}, err
	}
	definitions, err := profile.selectStrategies(input)
	if err != nil {
		return profile, InstalledZapretStrategies{}, err
	}
	if err := profile.verifyEngine(); err != nil {
		return profile, InstalledZapretStrategies{}, err
	}
	selected, err := z.resolve(definitions, input.Bindings)
	if err != nil {
		return profile, InstalledZapretStrategies{}, err
	}
	if len(selected.Intents) != len(input.Bindings) || len(selected.Families) != len(input.Bindings) {
		return profile, InstalledZapretStrategies{}, errors.New("installed Zapret did not resolve all packet scopes")
	}
	if selected.UsesCustomBlobs && !profile.AllowCustomBlobs {
		return profile, InstalledZapretStrategies{}, errZapretCustomBlobsDenied
	}
	if selected.needsReturn() {
		if err := verifyZapretReplyAsset(); err != nil {
			return profile, InstalledZapretStrategies{}, err
		}
	}
	for i, request := range input.Bindings {
		if selected.Families[i].RequiresIPv6 && !profile.IPv6 {
			return profile, InstalledZapretStrategies{}, fmt.Errorf("strategy for outbound %q requires IPv6 in the installed Zapret profile", request.OutboundRef)
		}
		if request.Outer.Kind != "" && !selected.handlesOuter(i, profile.IPv6, request.Outer) {
			return profile, InstalledZapretStrategies{}, fmt.Errorf("%w: %s %s outer port %d, family %d", errZapretOuterScope,
				request.Outer.Kind, request.Outer.Transports(), request.Outer.Port, request.Outer.Family)
		}
	}
	if err := selected.verifyDNSRequirements(ctx, profile.IPv6, input); err != nil {
		return profile, InstalledZapretStrategies{}, err
	}
	return profile, selected, nil
}

var errZapretDNSCoverage = errors.New("protected DNS scope is invalid")

func (s InstalledZapretStrategies) verifyDNSRequirements(ctx context.Context, ipv6 bool, input zapretactivation.Validation) error {
	if len(input.DNS) == 0 {
		return nil
	}
	used := 0
	for i, request := range input.Bindings {
		requirements, ok := input.DNS[request.OutboundRef]
		if !ok {
			continue
		}
		used++
		if request.Outer.Kind != "" || len(requirements) == 0 {
			return fmt.Errorf("%w: outbound %q must be a selected Direct with nonempty packet requirements", errZapretDNSCoverage, request.OutboundRef)
		}
		// The broker frame bounds the input. Coverage checks do not add
		// rules, queues or descriptors to the already selected packet path.
		for _, need := range requirements {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !s.handlesDNS(i, ipv6, need) {
				if need.PlainUDP {
					return fmt.Errorf("%w: strategy for Direct %q must cover UDP and its TCP retry on DNS port %d, family %d", errZapretDNSCoverage, request.OutboundRef, need.Port, need.Family)
				}
				return fmt.Errorf("%w: strategy for Direct %q does not cover port %d, family %d (QUIC %t)", errZapretDNSCoverage, request.OutboundRef, need.Port, need.Family, need.QUIC)
			}
		}
	}
	if len(input.DNS) != used {
		return fmt.Errorf("%w: DNS requirements must refer to a selected Direct", errZapretDNSCoverage)
	}
	return nil
}

func (s InstalledZapretStrategies) handlesDNS(binding int, ipv6 bool, need zapretactivation.DNSPathRequirement) bool {
	if need.Port == 0 || need.QUIC && need.PlainUDP {
		return false
	}
	covered := func(v6 bool) bool {
		ports := s.Families[binding].IPv4
		if v6 {
			ports = s.Families[binding].IPv6
		}
		udp := ports.Contains(intent.PacketProtocolUDP, need.Port)
		if need.QUIC {
			return udp && s.HandlesQUIC != nil && s.HandlesQUIC(binding, v6, need.Port)
		}
		return ports.Contains(intent.PacketProtocolTCP, need.Port) && (!need.PlainUDP || udp)
	}
	return coversZapretFamily(need.Family, ipv6, covered)
}

var errZapretOuterScope = errors.New("selected Zapret strategy does not cover the proxy's outer transport")

func (s InstalledZapretStrategies) handlesOuter(binding int, ipv6 bool, need zapretactivation.OuterSocket) bool {
	if !need.Valid() || need.Kind == "" {
		return false
	}
	return coversZapretFamily(need.Family, ipv6, func(v6 bool) bool {
		ports := s.Families[binding].IPv4
		if v6 {
			ports = s.Families[binding].IPv6
		}
		switch need.Transports() {
		case zapretactivation.OuterTCP:
			return ports.Contains(intent.PacketProtocolTCP, need.Port)
		case zapretactivation.OuterUDP:
			return ports.Contains(intent.PacketProtocolUDP, need.Port)
		case zapretactivation.OuterTCPUDP:
			return ports.Contains(intent.PacketProtocolTCP, need.Port) && ports.Contains(intent.PacketProtocolUDP, need.Port)
		default:
			return false
		}
	})
}

func coversZapretFamily(family zapretactivation.IPFamily, ipv6 bool, covered func(bool) bool) bool {
	switch family {
	case zapretactivation.FamilyFromProfile:
		return covered(false) && (!ipv6 || covered(true))
	case zapretactivation.FamilyIPv4:
		return covered(false)
	case zapretactivation.FamilyIPv6:
		return ipv6 && covered(true)
	default:
		return false
	}
}

func (z *installedZapret) prepare(ctx context.Context, batch zapretactivation.BatchRequest, peer PeerIdentity) (_ *installedZapretPath, failure error) {
	if err := checkZapretDescriptorBudget(len(batch.Bindings), batch.TUN != nil); err != nil {
		return nil, err
	}
	profile, selected, err := z.selectStrategies(ctx, batch.Validation)
	if err != nil {
		return nil, err
	}
	if err := profile.verifyTUN(ctx, batch.TUN); err != nil {
		return nil, err
	}
	replies := selected.needsReturn()
	egress, occupied, err := observeInstalledZapretNetwork(ctx, profile.Interface, profile.IPv6, batch.TUN != nil, replies, z.deny.port != 0, nil)
	if err != nil {
		return nil, err
	}
	if err := profile.verifyDialBindings(batch.Bindings, egress.IfIndex); err != nil {
		return nil, err
	}
	var tunScope *planner.ZapretTUNScope
	if batch.TUN != nil {
		if egress.RoutingMarkBits&planner.ZapretTUNForwardBit != 0 {
			return nil, errors.New("a foreign routing rule claims the Zapret TUN forward bit")
		}
		tunScope = &profile.TUN.Scope
	}
	grants := make([]zapretactivation.Grant, len(batch.Bindings))
	facts := make([]planner.Facts, len(batch.Bindings))
	for i, request := range batch.Bindings {
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil {
			return nil, err
		}
		generation := hex.EncodeToString(token[:])
		serial, err := z.journal.reserveZapretMark(ctx, peer, request, generation)
		if err != nil {
			return nil, err
		}
		facts[i] = installedZapretFacts(request, &egress, occupied, serial)
		facts[i].Zapret.SystemdFamilies = &selected.Families[i]
		facts[i].Zapret.SystemdReplyGuard = replies
		grants[i] = zapretactivation.Grant{Request: request, StrategyRevision: selected.Intents[i].StrategyRevision, Generation: generation}
	}
	plans, err := planner.CompileZapretSystemdBatch(selected.Intents, facts)
	if err != nil {
		return nil, err
	}
	args, trace, err := compileInstalledZapret(plans, selected, plans[0].Queue(), profile.ObserveNativeProfiles)
	if err != nil {
		return nil, err
	}
	if err := checkZapretMemoryBudget(ctx, args, 0); err != nil {
		return nil, err
	}
	hadDeny := z.deny.port != 0
	deny, err := z.openDeny(ctx, egress.NamespaceCookie, tunScope, replies)
	if err != nil {
		return nil, err
	}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, deny.Close())
			// This attempt could not have marked any socket. Keep an inherited
			// deny, which may still fence marks from the previous Core.
			if !hadDeny {
				cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := removeZapretDenyFromSystemd(cleanup, z.deny); err != nil {
					failure = errors.Join(failure, err)
				} else {
					z.deny = zapretDenyIdentity{}
				}
			}
		}
	}()
	var tun *preparedZapretTUN
	if tunScope != nil {
		tun, err = prepareZapretTUN(ctx, *tunScope, int(egress.IfIndex), batch.TUN.MTU, batch.BatchID)
		if err != nil {
			return nil, err
		}
		defer func() {
			if failure != nil {
				failure = errors.Join(failure, tun.close())
			}
		}()
	}
	engine, fact, err := startZapretEngine(ctx, plans[0], args, profile.EngineDigest, trace)
	if err != nil {
		return nil, err
	}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, engine.stop())
		}
	}()
	table, err := nftables.CreateZapretOwnedBatchForIngress(ctx, plans, tun.forwardPlan())
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stdout, "installed Zapret ruleset: bindings=%d nft_batch_bytes=%d\n", len(plans), table.BatchBytes())
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, table.Close())
		}
	}()
	if tun != nil {
		if err := tun.activate(); err != nil {
			return nil, err
		}
	}
	if replies {
		if err := checkZapretForeignHooks(ctx, tun != nil, true, true, table.Verify); err != nil {
			return nil, err
		}
	}
	peerFD, err := unix.FcntlInt(uintptr(peer.livenessFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	retainedPeer := peer
	retainedPeer.livenessFD = peerFD
	defer func() {
		if failure != nil {
			_ = unix.Close(peerFD)
		}
	}()
	lifetime, release := context.WithCancel(z.ctx)
	defer func() {
		if failure != nil {
			release()
		}
	}()
	authority, err := z.server.beginAuthority(lifetime)
	if err != nil {
		return nil, err
	}
	defer func() {
		if failure != nil {
			authority.Release()
		}
	}()
	if !authority.Revisioned || authority.Revision != peer.ManifestRevision || authority.Attestor.Recheck(ctx, peer, RolePanel) != nil {
		return nil, errZapretSocket
	}
	gid, err := lookupBrokerGroup()
	if err != nil {
		return nil, err
	}
	if err := makeZapretSocketRoot(gid); err != nil {
		return nil, err
	}
	var endpoints []*standaloneSocket
	var tunEndpoint *standaloneSocket
	defer func() {
		if failure != nil {
			if tunEndpoint != nil {
				failure = errors.Join(failure, tunEndpoint.close())
			}
			for _, endpoint := range endpoints {
				failure = errors.Join(failure, endpoint.close())
			}
			_ = os.Remove(zapretSocketRoot) // only removes an empty owned directory
		}
	}()
	socketGrants := make([]ZapretSocketGrant, len(grants))
	for i, grant := range grants {
		scope, err := plans[i].SocketScope()
		if err != nil {
			return nil, err
		}
		socketGrants[i] = ZapretSocketGrant{Scope: scope, Generation: grant.Generation, PeerRevision: peer.Revision}
	}
	endpoints, err = bindZapretSocketBatch(ctx, zapretSocketRoot, gid, socketGrants)
	if err != nil {
		return nil, err
	}
	if tun != nil {
		tunEndpoint, err = bindZapretEndpoint(ctx, zapretSocketRoot, gid, "t-", batch.BatchID)
		if err != nil {
			return nil, err
		}
	}
	if ctx.Err() != nil || authority.Context.Err() != nil || table.Verify(ctx) != nil || deny.Verify(ctx) != nil {
		return nil, errors.New("installed Zapret generation changed before publication")
	}
	path := &installedZapretPath{ctx: authority.Context, release: release, done: make(chan struct{}), peer: peer.Revision, uid: peer.UID,
		queue: plans[0].Queue(), enginePID: fact.PID, trace: trace,
		grant: zapretactivation.BatchGrant{BatchID: batch.BatchID, Bindings: grants, TUN: batch.TUN}}
	// Observation and FD admission check the same table and queue owners.
	// Observation never creates a lease or extends the batch lifetime.
	path.verify = func(ctx context.Context) error {
		if ctx.Err() != nil || table.Verify(ctx) != nil || deny.Verify(ctx) != nil {
			return errZapretSocket
		}
		if err := profile.verifyDialBindings(batch.Bindings, egress.IfIndex); err != nil {
			return err
		}
		_, err := nftables.ObserveZapretQueueOwner(plans[0], fact)
		return err
	}
	go func() {
		defer close(path.done)
		defer authority.Release()
		defer unix.Close(peerFD)
		defer release()
		defer deny.Close()
		defer func() {
			path.stopErr = errors.Join(path.stopErr, engine.stop())
			if path.stopErr != nil {
				fmt.Fprintln(os.Stderr, "installed Zapret engine retirement:", path.stopErr)
			}
		}()
		// Cancel the accepting side on engine death; close the admission
		// table before stopping the engine for a normal retirement.
		watch := make(chan struct{})
		go func() {
			defer close(watch)
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-path.ctx.Done():
					return
				case <-engine.done:
					if path.ctx.Err() == nil {
						fmt.Fprintln(os.Stderr, "installed Zapret batch lost its engine:", engine.waitErr)
					}
					release()
					return
				case <-ticker.C:
					if err := peerAlive(retainedPeer); err != nil {
						fmt.Fprintln(os.Stderr, "installed Zapret batch lost its Panel peer:", err)
						release()
						return
					}
				}
			}
		}()
		stop := context.AfterFunc(path.ctx, func() { _ = table.Close() })
		fence := func(ctx context.Context, _ ZapretSocketGrant, _ int, admit func() error) error {
			path.mu.Lock()
			defer path.mu.Unlock()
			if peerAlive(retainedPeer) != nil || path.verify(ctx) != nil {
				fmt.Fprintln(os.Stderr, "installed Zapret batch lost its socket admission fence")
				release()
				return errZapretSocket
			}
			return admit()
		}
		var listeners sync.WaitGroup
		if tunEndpoint != nil {
			listeners.Go(func() {
				if err := z.server.serveZapretTUN(path.ctx, tunEndpoint, retainedPeer, tun.file, path.verify); err != nil && path.ctx.Err() == nil {
					fmt.Fprintln(os.Stderr, "installed Zapret TUN handoff:", err)
					release()
				}
			})
		}
		for i, endpoint := range endpoints {
			listeners.Go(func() {
				err := z.server.serveZapretSockets(path.ctx, endpoint, socketGrants[i], fence, func(context.Context, string) error { return nil })
				if path.ctx.Err() == nil {
					fmt.Fprintln(os.Stderr, "installed Zapret batch lost a socket listener:", err)
				}
				release() // The batch cannot advertise a subset of its bindings.
			})
		}
		listeners.Wait()
		release()
		<-watch
		stop()
		_ = table.Close()
		if tun != nil {
			path.stopErr = tun.close()
		}
		_ = os.Remove(zapretSocketRoot) // only succeeds after the last socket has been unlinked
	}()
	return path, nil
}

// openDeny reuses only the exact kernel owner socket retained by PID 1.
// A new generation grants a new mark while the same deny continues to fence
// marks issued by previous generations, including across broker death.
func (z *installedZapret) openDeny(ctx context.Context, namespace uint64, tun *planner.ZapretTUNScope, replies bool) (*nftables.ZapretOwnedTable, error) {
	if z.deny.port != 0 {
		return nftables.ObserveZapretDenyTableForIngress(ctx, namespace, z.server.BootID, z.deny.port, z.deny.inode, tun, replies)
	}
	owner, err := nftables.CreateZapretDenyTableForIngress(ctx, namespace, z.server.BootID, tun, replies)
	if err != nil {
		return nil, err
	}
	err = owner.WithZapretDenyOwner(func(fd int) error {
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			return err
		}
		address, err := unix.Getsockname(fd)
		netlink, ok := address.(*unix.SockaddrNetlink)
		if err != nil || !ok || netlink.Pid == 0 {
			return errors.New("no bound netlink port for the Zapret deny owner")
		}
		if err := keepZapretDenyInSystemd(ctx, fd); err != nil {
			return err
		}
		z.deny = zapretDenyIdentity{port: netlink.Pid, inode: stat.Ino}
		return nil
	})
	if err != nil {
		return nil, errors.Join(err, owner.Close())
	}
	return owner, nil
}

func makeZapretSocketRoot(gid uint32) error {
	if err := os.Mkdir(zapretSocketRoot, 0o750); err == nil {
		if err := os.Chown(zapretSocketRoot, 0, int(gid)); err != nil {
			return err
		}
		if err := os.Chmod(zapretSocketRoot, 0o750); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := verifyStandaloneDirectory(zapretSocketRoot, gid); err != nil {
		return err
	}
	// No prior generation is accepting sockets here. Remove only our socket
	// nodes left by a killed broker from the root-owned directory.
	entries, err := os.ReadDir(zapretSocketRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		generation, ok := strings.CutPrefix(entry.Name(), "z-")
		if !ok {
			generation, ok = strings.CutPrefix(entry.Name(), "t-")
		}
		if !ok || !digestPattern.MatchString(generation) {
			return errors.New("unexpected path in the Zapret socket directory")
		}
		path := filepath.Join(zapretSocketRoot, entry.Name())
		var stat unix.Stat_t
		if err := unix.Lstat(path, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFSOCK || stat.Uid != 0 || stat.Gid != gid {
			return errors.New("unexpected socket owner in the Zapret socket directory")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func startZapretEngine(ctx context.Context, plan planner.ZapretPlan, args []string, engineDigest string, trace ZapretNativeTrace) (_ *installedZapretEngine, _ processevidence.Fact, failure error) {
	credential := fmt.Sprintf("--uid=%d:%d", zapretEngineUID, zapretEngineUID)
	// Match the admitted socket's egress for generated packets. A raw
	// send via an alternate route can fail and make nfqws2 pass the original.
	scope, err := plan.SocketScope()
	if err != nil {
		return nil, processevidence.Fact{}, err
	}
	args = append(args, "--bind-fix4")
	if scope.Egress().IPv6Gateway.IsValid() {
		args = append(args, "--bind-fix6")
	}
	cmd := exec.Command(zapretEnginePath, append(args, credential)...)
	cmd.Dir = zapretEngineRoot
	cmd.Env = []string{"LANG=C", "LC_ALL=C"}
	if trace != nil {
		cmd.Stdout = trace
		cmd.WaitDelay = 250 * time.Millisecond
	}
	pidfd := -1
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, PidFD: &pidfd}
	if err := cmd.Start(); err != nil {
		return nil, processevidence.Fact{}, err
	}
	engine := &installedZapretEngine{process: cmd.Process, pidfd: os.NewFile(uintptr(pidfd), "nfqws2"), done: make(chan struct{})}
	go func() { engine.waitErr = cmd.Wait(); close(engine.done) }()
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, engine.stop())
		}
	}()
	if pidfd < 0 {
		return nil, processevidence.Fact{}, errors.New("the installed Zapret engine requires a pidfd")
	}
	ticker := time.NewTicker(35 * time.Millisecond)
	defer ticker.Stop()
	ready, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var cause error
	for {
		fact, err := processevidence.Observe(cmd.Process.Pid)
		if err == nil && fact.ExeDigest == engineDigest {
			if owner, err := nftables.ObserveZapretQueueOwner(plan, fact); err == nil {
				fmt.Fprintln(os.Stdout, "installed Zapret NFQUEUE ready", strconv.Itoa(int(owner.Queue)), owner.ProcessRevision)
				return engine, fact, nil
			} else {
				cause = err
			}
		} else {
			cause = err
		}
		select {
		case <-engine.done:
			return nil, processevidence.Fact{}, errors.Join(errors.New("pinned nfqws2 exited before binding NFQUEUE"), engine.waitErr, cause)
		case <-ready.Done():
			return nil, processevidence.Fact{}, errors.Join(errors.New("pinned nfqws2 queue readiness could not be established"), cause)
		case <-ticker.C:
		}
	}
}
