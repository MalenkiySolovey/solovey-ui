package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	"github.com/MalenkiySolovey/solovey-ui/internal/evidencebundle"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "solovey privileged broker:", err)
		os.Exit(1)
	}
}

func run() error {
	composition, err := parseRuntimeComposition(os.Args[1:])
	if err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return errors.New("the broker must run as root")
	}
	manifestPath, err := broker.ManifestPathForTransport(composition.transport)
	if err != nil {
		return err
	}
	manifest, err := loadAuthorizedManifest(manifestPath)
	if err != nil {
		return err
	}
	bootIDBytes, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return errors.New("kernel boot identity is unavailable")
	}
	bootID := string(bytesTrimSpace(bootIDBytes))
	journal, err := broker.OpenFileJournal(broker.DefaultJournalRoot, bootID)
	if err != nil {
		return err
	}
	registry := broker.NewRegistry()
	if err := registerHandlerGraph(registry, composition, journal, productionHandlerRegistrars()); err != nil {
		return err
	}
	attestor, err := broker.NewManifestAttestor(manifest)
	if err != nil {
		return err
	}
	server, err := broker.NewServer(registry, journal, attestor, bootID)
	if err != nil {
		return err
	}
	if err := attachRecentDiagnosticRing(server); err != nil {
		return err
	}
	if err := attachArmedDiagnosticCapture(server); err != nil {
		return err
	}
	listenerSet, err := broker.OpenTransport(composition.transport)
	if err != nil {
		return err
	}
	listeners := listenerSet.Listeners
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	auditQueue := make(chan broker.AuditEvent, 128)
	var droppedAuditEvents atomic.Uint64
	auditDone := make(chan struct{})
	auditStop := make(chan struct{})
	go func() {
		defer close(auditDone)
		write := func(event broker.AuditEvent) {
			// AuditEvent is a closed safe-facts schema; journald/stdout never
			// receives the broker payload or host-command output.
			if encoded, marshalErr := json.Marshal(event); marshalErr == nil {
				fmt.Fprintln(os.Stdout, "solovey-broker-audit", string(encoded))
			}
		}
		for {
			select {
			case event := <-auditQueue:
				write(event)
			case <-auditStop:
				for {
					select {
					case event := <-auditQueue:
						write(event)
					default:
						return
					}
				}
			}
		}
	}()
	server.Audit = func(event broker.AuditEvent) {
		if dropped := droppedAuditEvents.Swap(0); dropped > 0 {
			saturation := broker.AuditEvent{Timestamp: time.Now().UTC().Unix(), OwnerDomain: "broker",
				PeerRole: "unknown", ResultClass: "audit_queue_saturated", DurationClass: "not_measured",
				RevisionTransition: "none", RecoveryClass: "none", AggregateCount: dropped}
			select {
			case auditQueue <- saturation:
			default:
				droppedAuditEvents.Add(dropped)
			}
		}
		select {
		case auditQueue <- event:
		default:
			droppedAuditEvents.Add(1)
		}
	}
	errorsChannel := make(chan error, len(listeners))
	authorityErrors := make(chan error, 1)
	go func() { authorityErrors <- watchAuthorityManifest(ctx, server, manifest.Revision, manifestPath) }()
	var group sync.WaitGroup
	for role, listener := range listeners {
		role, listener := role, listener
		group.Add(1)
		go func() {
			defer group.Done()
			errorsChannel <- server.Serve(ctx, listener, role)
		}()
	}
	select {
	case <-ctx.Done():
		_ = listenerSet.Close()
		group.Wait()
		server.ShutdownConnections()
		server.WaitConnections()
		close(auditStop)
		<-auditDone
		return nil
	case err := <-errorsChannel:
		cancel()
		_ = listenerSet.Close()
		group.Wait()
		server.ShutdownConnections()
		server.WaitConnections()
		close(auditStop)
		<-auditDone
		return err
	case err := <-authorityErrors:
		cancel()
		_ = listenerSet.Close()
		group.Wait()
		server.ShutdownConnections()
		server.WaitConnections()
		close(auditStop)
		<-auditDone
		return err
	}
}

func attachArmedDiagnosticCapture(server *broker.Server) error {
	return attachArmedDiagnosticCaptureAt(server, evidencebundle.DefaultCaptureRoot)
}

func attachArmedDiagnosticCaptureAt(server *broker.Server, root string) error {
	recorder, err := evidencebundle.OpenArmed(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("armed diagnostic capture is invalid: %w", err)
	}
	armedSink := func(event broker.DiagnosticEvent) {
		failureClass := string(event.PeerAttestation)
		if event.HandlerReason != "" {
			failureClass = event.HandlerReason
		}
		if failureClass == "" {
			failureClass = event.ResultClass
		}
		service, instance := event.Peer.ProcdService, event.Peer.ProcdInstance
		if service == "" {
			service = event.Peer.CgroupUnit
		}
		if instance == "" {
			instance = event.Peer.ManifestClient
		}
		snapshot := evidencebundle.BrokerSnapshot{
			Timestamp: event.Timestamp, FailureClass: failureClass, Role: string(event.PeerRole),
			PID: event.Peer.PID, UID: event.Peer.UID, GID: event.Peer.GID,
			ExecutableLabel: event.Peer.Executable, ExecutableSHA256: event.Peer.ExecutableDigest,
			ExecutableDevice: event.Peer.Device, ExecutableInode: event.Peer.Inode,
			ExecutableMode: event.Peer.ExecutableMode, ExecutableUID: event.Peer.ExecutableUID,
			ExecutableGID: event.Peer.ExecutableGID, Service: service, Instance: instance,
			CgroupAvailability: event.Peer.CgroupAvailability, CgroupPolicy: string(event.Peer.CgroupPolicy),
			CgroupUnit: event.Peer.CgroupUnit, CgroupAuthorityRevision: event.Peer.CgroupAuthorityRevision,
			SupervisorCgroup: event.Peer.SupervisorCgroup, Supervisor: event.Peer.Supervisor,
			SupervisorPID: event.Peer.SupervisorPID, SupervisorStart: event.Peer.SupervisorStart,
			BootID: event.Peer.BootID, StartIdentity: event.Peer.StartTime,
			ManifestClient: event.Peer.ManifestClient, ManifestRevision: event.Peer.ManifestRevision,
			HandlerOwner: event.HandlerOwner, HandlerStage: event.HandlerStage, HandlerReason: event.HandlerReason,
			HandlerErrno: event.HandlerErrno, ProofMethod: event.ProofMethod,
		}
		_ = recorder.CaptureFailure(context.Background(), evidencebundle.Trigger{
			FailureClass: failureClass, Timestamp: event.Timestamp, TargetTimestamp: event.Timestamp,
			ControllerTimestamp: time.Now().UTC(),
		}, evidencebundle.BrokerCollectors(snapshot))
	}
	server.Diagnostic = chainedDiagnosticSink(server.Diagnostic, armedSink)
	return nil
}

func attachRecentDiagnosticRing(server *broker.Server) error {
	if server == nil {
		return errors.New("recent diagnostic broker is unavailable")
	}
	ring, err := broker.OpenRecentDiagnosticRing()
	if err != nil {
		return errors.New("recent diagnostic ring is unavailable")
	}
	server.Diagnostic = chainedDiagnosticSink(server.Diagnostic, func(event broker.DiagnosticEvent) {
		if err := ring.Record(event); err != nil {
			// The error is intentionally not interpolated: filesystem paths and
			// arbitrary host error strings never enter broker output.
			fmt.Fprintln(os.Stderr, "solovey-broker-diagnostic write_failed")
		}
	})
	return nil
}

func chainedDiagnosticSink(first, second func(broker.DiagnosticEvent)) func(broker.DiagnosticEvent) {
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}
	return func(event broker.DiagnosticEvent) {
		first(event)
		second(event)
	}
}

func loadAuthorizedManifest(path string) (broker.Manifest, error) {
	manifest, err := broker.LoadManifest(path)
	if err != nil {
		return broker.Manifest{}, err
	}
	if manifest.RequiresProcd() {
		owner, err := deploymentidentity.LoadProcdInstalled()
		if err != nil || owner.Revision != manifest.ApplicationOwnerRevision {
			return broker.Manifest{}, errors.New("procd broker manifest differs from the application owner contract")
		}
	}
	return manifest, nil
}

func watchAuthorityManifest(ctx context.Context, server *broker.Server, current, path string) error {
	return watchAuthorityManifestWith(ctx, server, current, time.Second, func() (broker.Manifest, error) {
		return loadAuthorizedManifest(path)
	})
}

func watchAuthorityManifestWith(ctx context.Context, server *broker.Server, current string, interval time.Duration,
	load func() (broker.Manifest, error)) error {
	if server == nil || current == "" || interval <= 0 || load == nil {
		return errors.New("broker authority watcher is not configured")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			manifest, err := load()
			if err != nil {
				return errors.New("broker authority generation reload failed")
			}
			if manifest.Revision == current {
				continue
			}
			attestor, err := broker.NewManifestAttestor(manifest)
			if err != nil {
				return errors.New("broker authority generation attestor is invalid")
			}
			rotateContext, cancel := context.WithTimeout(ctx, 30*time.Second)
			err = server.RotateAuthority(rotateContext, attestor, manifest.Revision)
			cancel()
			if err != nil {
				return errors.New("broker authority generation rotation failed")
			}
			current = manifest.Revision
		}
	}
}

func bytesTrimSpace(value []byte) []byte {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\n' || value[start] == '\r' || value[start] == '\t') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\n' || value[end-1] == '\r' || value[end-1] == '\t') {
		end--
	}
	return value[start:end]
}
