package helper

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
)

type fakeSSHRecoveryOwner struct {
	support sshbroker.RecoverySupport
	result  *sshbroker.RecoveryResult
	err     error
	request sshbroker.RecoveryObserveRequest
}

func (f *fakeSSHRecoveryOwner) Detect(context.Context) sshbroker.RecoverySupport { return f.support }

func (f *fakeSSHRecoveryOwner) Observe(_ context.Context, request sshbroker.RecoveryObserveRequest) (*sshbroker.RecoveryResult, error) {
	f.request = request
	return f.result, f.err
}

func TestSSHRecoveryAdapterProjectsOnlyTypedOwnerCapabilityAndFacts(t *testing.T) {
	verifier, observer := strings.Repeat("a", 64), strings.Repeat("b", 64)
	owner := &fakeSSHRecoveryOwner{support: sshbroker.RecoverySupport{PlatformKnown: true, Linux: true, Available: true,
		EvidenceKind: sshbroker.LogEvidenceJournald, VerifierRevision: verifier, ObserverRevision: observer},
		result: &sshbroker.RecoveryResult{VerifierRevision: verifier, ObserverRevision: observer, Observations: []sshbroker.RecoveryObservation{{
			ObservationID: "recovery:" + strings.Repeat("c", 64), PrincipalID: "principal:" + strings.Repeat("d", 64),
			SourcePrefix: "192.0.2.4/32", AuthenticationClass: "publickey", ObservedAt: 100, ObservedAtMicros: 100_000_001}}}}
	executor := newSSHRecoveryExecutorFromOwner(owner)
	support := executor.Detect(context.Background())
	if !support.Available || support.EvidenceKind != SSHRecoveryEvidenceJournald || support.VerifierRevision != verifier || support.ObserverRevision != observer {
		t.Fatalf("owner support projection changed: %#v", support)
	}
	request := SSHRecoveryObserveRequest{SinceUnixMicros: 99_000_000, MaxEvents: 7}
	result, err := executor.Observe(context.Background(), request)
	if err != nil || result.VerifierRevision != verifier || result.ObserverRevision != observer || len(result.Observations) != 1 {
		t.Fatalf("owner result projection changed: result=%#v err=%v", result, err)
	}
	if owner.request.SinceUnixMicros != request.SinceUnixMicros || owner.request.MaxEvents != request.MaxEvents ||
		result.Observations[0].SourcePrefix != "192.0.2.4/32" || result.Observations[0].AuthenticationClass != "publickey" {
		t.Fatalf("owner request/fact projection changed: request=%#v result=%#v", owner.request, result)
	}
}

func TestSSHRecoveryCapabilityRequiresCompleteOwnerRevisionProjection(t *testing.T) {
	valid := sshbroker.RecoverySupport{PlatformKnown: true, Linux: true, Available: true, EvidenceKind: sshbroker.LogEvidenceLogread,
		VerifierRevision: strings.Repeat("a", 64), ObserverRevision: strings.Repeat("b", 64)}
	for name, mutate := range map[string]func(*sshbroker.RecoverySupport){
		"missing verifier": func(value *sshbroker.RecoverySupport) { value.VerifierRevision = "" },
		"missing observer": func(value *sshbroker.RecoverySupport) { value.ObserverRevision = "" },
		"unknown evidence": func(value *sshbroker.RecoverySupport) { value.EvidenceKind = "unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			engine := ContractEngine{root: testManagedRoot(t), sshRecoveryExecutor: newSSHRecoveryExecutorFromOwner(&fakeSSHRecoveryOwner{support: candidate})}
			capabilities := engine.capabilities(context.Background())
			if CapabilityAvailable(capabilities, OperationSSHRecoveryObserve) {
				t.Fatalf("incomplete SSH owner projection was advertised: %#v", capabilities.SSHRecovery)
			}
		})
	}
	engine := ContractEngine{root: testManagedRoot(t), sshRecoveryExecutor: newSSHRecoveryExecutorFromOwner(&fakeSSHRecoveryOwner{support: valid})}
	if capabilities := engine.capabilities(context.Background()); !CapabilityAvailable(capabilities, OperationSSHRecoveryObserve) {
		t.Fatalf("complete SSH owner projection was not advertised: %#v", capabilities.SSHRecovery)
	}
}

func TestSSHRecoveryAdapterPropagatesOwnerFailure(t *testing.T) {
	expected := errors.New("owner failed")
	owner := &fakeSSHRecoveryOwner{err: expected}
	if _, err := newSSHRecoveryExecutorFromOwner(owner).Observe(context.Background(), SSHRecoveryObserveRequest{}); !errors.Is(err, expected) {
		t.Fatalf("owner error changed: %v", err)
	}
}

func TestSSHRecoveryBrokerRegistrationRejectsAnUnresolvedRecord(t *testing.T) {
	err := RegisterBrokerHandlersWithSSHComposition(broker.NewRegistry(), testManagedRoot(t), sshbroker.ResolvedSSHComposition{})
	if err == nil {
		t.Fatal("Server Protection accepted the zero-value unresolved SSH composition")
	}
}

func TestSSHRecoveryTransportProjectionIsBoundedCanonicalAndRevisionComplete(t *testing.T) {
	now := time.Unix(1_800_000_000, 500_000_000).UTC()
	request := SSHRecoveryObserveRequest{SinceUnixMicros: now.Add(-time.Minute).UnixMicro(), MaxEvents: 2}
	valid := &SSHRecoveryResult{VerifierRevision: strings.Repeat("a", 64), ObserverRevision: strings.Repeat("b", 64), Observations: []SSHRecoveryObservation{{
		ObservationID: "recovery:" + strings.Repeat("c", 64), PrincipalID: "principal:" + strings.Repeat("d", 64),
		SourcePrefix: "192.0.2.4/32", AuthenticationClass: "publickey", ObservedAt: now.Unix(), ObservedAtMicros: now.UnixMicro(),
	}}}
	if !ValidSSHRecoveryResult(request, valid, now) {
		t.Fatal("complete SSH owner projection was rejected")
	}
	for name, mutate := range map[string]func(*SSHRecoveryResult){
		"missing observer revision": func(result *SSHRecoveryResult) { result.ObserverRevision = "" },
		"private principal":         func(result *SSHRecoveryResult) { result.Observations[0].PrincipalID = "alice" },
		"noncanonical prefix":       func(result *SSHRecoveryResult) { result.Observations[0].SourcePrefix = "192.0.2.4/24" },
		"wrong authentication":      func(result *SSHRecoveryResult) { result.Observations[0].AuthenticationClass = "password" },
		"stale observation":         func(result *SSHRecoveryResult) { result.Observations[0].ObservedAtMicros = request.SinceUnixMicros },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := *valid
			candidate.Observations = append([]SSHRecoveryObservation(nil), valid.Observations...)
			mutate(&candidate)
			if ValidSSHRecoveryResult(request, &candidate, now) {
				t.Fatalf("invalid SSH recovery projection was accepted: %#v", candidate)
			}
		})
	}
}

func TestCapabilityNegotiationRejectsIncompleteAdvertisedRecoveryOwner(t *testing.T) {
	capabilities := DefaultCapabilities()
	capabilities.SSHRecovery = SSHRecoverySupport{PlatformKnown: true, Linux: true, Available: true,
		EvidenceKind: SSHRecoveryEvidenceJournald, VerifierRevision: strings.Repeat("a", 64)}
	for index := range capabilities.Capabilities {
		if capabilities.Capabilities[index].Operation == OperationSSHRecoveryObserve {
			capabilities.Capabilities[index].Available = true
			capabilities.Capabilities[index].Reason = ""
		}
	}
	setCapabilityRevision(capabilities)
	if ValidateCapabilities(capabilities) == nil {
		t.Fatal("capability negotiation accepted an advertised SSH recovery owner without observer revision")
	}
	capabilities.SSHRecovery.ObserverRevision = strings.Repeat("b", 64)
	setCapabilityRevision(capabilities)
	if err := ValidateCapabilities(capabilities); err != nil {
		t.Fatalf("complete SSH recovery owner capability was rejected: %v", err)
	}
}
