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

	deploymentbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/deploymentbroker"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
	updatebroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "solovey privileged broker:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 1 {
		return errors.New("arguments are not supported")
	}
	if os.Geteuid() != 0 {
		return errors.New("the broker must run as root")
	}
	manifest, err := broker.LoadManifest(broker.DefaultManifest)
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
	if err := broker.RegisterContributedHandlers(registry); err != nil {
		return err
	}
	if err := sshbroker.RegisterHandlers(registry); err != nil {
		return err
	}
	if err := deploymentbroker.RegisterHandlers(registry); err != nil {
		return err
	}
	if err := updatebroker.RegisterHandlers(registry); err != nil {
		return err
	}
	server, err := broker.NewServer(registry, journal, broker.ManifestAttestor{Manifest: manifest}, bootID)
	if err != nil {
		return err
	}
	listeners, err := broker.ActivatedListeners()
	if err != nil {
		return err
	}
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
		for _, listener := range listeners {
			_ = listener.Close()
		}
		group.Wait()
		server.ShutdownConnections()
		server.WaitConnections()
		close(auditStop)
		<-auditDone
		return nil
	case err := <-errorsChannel:
		cancel()
		for _, listener := range listeners {
			_ = listener.Close()
		}
		group.Wait()
		server.ShutdownConnections()
		server.WaitConnections()
		close(auditStop)
		<-auditDone
		return err
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
