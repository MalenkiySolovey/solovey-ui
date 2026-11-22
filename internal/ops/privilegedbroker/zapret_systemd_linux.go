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
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

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
	targets []zapretactivation.Request
	grant   zapretactivation.BatchGrant
	peer    string
	uid     uint32
	ctx     context.Context
	release context.CancelFunc
	done    chan struct{}
	stopErr error // published by closing done
	mu      sync.Mutex
}

func RegisterInstalledZapret(ctx context.Context, server *Server, transport TransportMode, listeners *ListenerSet, resolve ResolveInstalledZapretStrategies) error {
	if transport != SystemdActivated {
		return nil
	}
	journal, ok := server.Journal.(*FileJournal)
	if !ok || listeners == nil || resolve == nil {
		return errors.New("installed Zapret requires the broker file journal and socket activation")
	}
	owner := &installedZapret{ctx: ctx, server: server, journal: journal, resolve: resolve, deny: listeners.deny}
	if err := server.Registry.Register(VerbZapretValidate, Definition{Role: RolePanel, Handler: owner.validate}); err != nil {
		return err
	}
	if err := server.Registry.Register(VerbZapretAcquire, Definition{Role: RolePanel, LocalPathAuthority: true, Handler: owner.acquire}); err != nil {
		return err
	}
	return server.Registry.Register(VerbZapretRelease, Definition{Role: RolePanel, LocalPathAuthority: true, Handler: owner.retire})
}

func (z *installedZapret) validate(_ context.Context, call Request, peer PeerIdentity) (any, error) {
	var input zapretactivation.Validation
	if DecodePayload(call.Payload, &input) != nil || !validZapretTargets(input.Bindings) ||
		peer.UID == 0 || peer.Supervisor != "systemd" || peer.CgroupPolicy != CgroupRequired || !peer.hasLiveness {
		return nil, Failure(CodeValidation, "invalid installed Zapret binding or Panel identity")
	}
	if _, _, err := z.selectStrategies(input.Bindings); err != nil {
		fmt.Fprintln(os.Stderr, "installed Zapret profile validation:", err)
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
		if existing.peer == peer.Revision && existing.grant.BatchID == target.BatchID && slices.Equal(existing.targets, target.Bindings) {
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
		return nil, Failure(CodeExecution, "Zapret packet path not ready; see privileged broker journal")
	}
	z.active = path
	return path.grant, nil
}

func validZapretTargets(targets []zapretactivation.Request) bool {
	if len(targets) == 0 || len(targets) > zapretactivation.MaxBindings {
		return false
	}
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		if !safeIdentifier(target.OutboundRef) || !safeIdentifier(target.StrategyRef) ||
			!digestPattern.MatchString(target.OutboundRevision) || seen[target.OutboundRef] {
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

func (z *installedZapret) selectStrategies(targets []zapretactivation.Request) (installedZapretProfile, InstalledZapretStrategies, error) {
	profile, err := readInstalledZapretProfile()
	if err != nil {
		return profile, InstalledZapretStrategies{}, err
	}
	definitions, err := profile.selectStrategies(targets)
	if err != nil {
		return profile, InstalledZapretStrategies{}, err
	}
	if err := profile.verifyEngine(); err != nil {
		return profile, InstalledZapretStrategies{}, err
	}
	selected, err := z.resolve(definitions, targets)
	return profile, selected, err
}

func (z *installedZapret) prepare(ctx context.Context, batch zapretactivation.BatchRequest, peer PeerIdentity) (_ *installedZapretPath, failure error) {
	profile, selected, err := z.selectStrategies(batch.Bindings)
	if err != nil {
		return nil, err
	}
	egress, occupied, err := observeInstalledZapretNetwork(ctx, profile.Interface)
	if err != nil {
		return nil, err
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
		grants[i] = zapretactivation.Grant{Request: request, StrategyRevision: selected.Intents[i].StrategyRevision, Generation: generation}
	}
	plans, err := planner.CompileZapretSystemdBatch(selected.Intents, facts)
	if err != nil {
		return nil, err
	}
	marks := make([]uint32, len(plans))
	for i, plan := range plans {
		marks[i] = plan.QueueMark()
	}
	args, err := selected.Compile(zapretEngineRoot, plans[0].Queue(), plans[0].LoopMark(), marks)
	if err != nil {
		return nil, err
	}
	hadDeny := z.deny.port != 0
	deny, err := z.openDeny(ctx, egress.NamespaceCookie)
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
	engine, fact, err := startZapretEngine(ctx, plans[0], args, profile.EngineDigest)
	if err != nil {
		return nil, err
	}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, engine.stop())
		}
	}()
	table, err := nftables.CreateZapretOwnedBatch(ctx, plans)
	if err != nil {
		return nil, err
	}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, table.Close())
		}
	}()
	peerFD, err := unix.FcntlInt(uintptr(peer.livenessFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
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
	endpoints := make([]*standaloneSocket, 0, len(grants))
	defer func() {
		if failure != nil {
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
		endpoint, err := bindZapretSocket(ctx, zapretSocketRoot, gid, grant.Generation)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
		socketGrants[i] = ZapretSocketGrant{Scope: scope, Generation: grant.Generation, PeerRevision: peer.Revision}
	}
	if ctx.Err() != nil || authority.Context.Err() != nil || table.Verify(ctx) != nil || deny.Verify(ctx) != nil {
		return nil, errors.New("installed Zapret generation changed before publication")
	}
	path := &installedZapretPath{ctx: authority.Context, release: release, done: make(chan struct{}), peer: peer.Revision, uid: peer.UID,
		targets: slices.Clone(batch.Bindings), grant: zapretactivation.BatchGrant{BatchID: batch.BatchID, Bindings: grants}}
	go func() {
		defer close(path.done)
		defer authority.Release()
		defer unix.Close(peerFD)
		defer release()
		defer deny.Close()
		defer func() {
			path.stopErr = engine.stop()
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
					if !zapretPeerAlive(peerFD) {
						fmt.Fprintln(os.Stderr, "installed Zapret batch lost its Panel peer")
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
			if ctx.Err() != nil || !zapretPeerAlive(peerFD) || table.Verify(ctx) != nil || deny.Verify(ctx) != nil {
				fmt.Fprintln(os.Stderr, "installed Zapret batch lost its socket admission fence")
				release()
				return errZapretSocket
			}
			if _, err := nftables.ObserveZapretQueueOwner(plans[0], fact); err != nil {
				fmt.Fprintln(os.Stderr, "installed Zapret batch lost its queue owner:", err)
				release()
				return err
			}
			return admit()
		}
		var listeners sync.WaitGroup
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
		_ = os.Remove(zapretSocketRoot) // only succeeds after the last socket has been unlinked
	}()
	return path, nil
}

// openDeny reuses only the exact kernel owner socket retained by PID 1.
// A new generation grants a new mark while the same deny continues to fence
// marks issued by previous generations, including across broker death.
func (z *installedZapret) openDeny(ctx context.Context, namespace uint64) (*nftables.ZapretOwnedTable, error) {
	if z.deny.port != 0 {
		return nftables.ObserveZapretDenyTable(ctx, namespace, z.server.BootID, z.deny.port, z.deny.inode)
	}
	owner, err := nftables.CreateZapretDenyTable(ctx, namespace, z.server.BootID)
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

func zapretPeerAlive(fd int) bool {
	n, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 0)
	return n == 0 && err == nil
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

func startZapretEngine(ctx context.Context, plan planner.ZapretPlan, args []string, engineDigest string) (_ *installedZapretEngine, _ processevidence.Fact, failure error) {
	credential := fmt.Sprintf("--uid=%d:%d", zapretEngineUID, zapretEngineUID)
	// Match the admitted socket's egress for generated IPv4 packets. A raw
	// send via an alternate route can fail and make nfqws2 pass the original.
	cmd := exec.Command(zapretEnginePath, append(args, "--bind-fix4", credential)...)
	cmd.Dir = zapretEngineRoot
	cmd.Env = []string{"LANG=C", "LC_ALL=C"}
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
