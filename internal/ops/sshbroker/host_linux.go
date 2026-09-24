//go:build linux

package sshbroker

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/listenerevidence"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

const (
	maxCommandOutput        = 256 << 10
	maxProcSocketTableBytes = int64(4 << 20)
)

var acceptedLogin = regexp.MustCompile(`^Accepted (publickey) for ([A-Za-z0-9._-]{1,64}) from ([0-9A-Fa-f:.]{2,64}) port ([0-9]{1,5})`)

var errRequiredFixedHostBinaryUnavailable = errors.New("required fixed host binary is unavailable")
var errDropbearBinaryUnavailable = errors.New("selected Dropbear binary is unavailable")

type Host struct {
	implementation Implementation
	stageAuthority broker.CompletedMutationAuthority
	service        sshServiceControlAdapter
	logs           sshLogEvidenceAdapter
	sshd           *executableobject.Object
	now            func() time.Time
	dropbear       *dropbearUCIHost
	// observePosture is a private owner-local observation seam. Production
	// leaves it nil and uses the selected concrete adapter below; focused
	// contracts may inject a bounded failure before public sanitization.
	observePosture func(context.Context) (ObservationV1, error)
}

type proofTicketV1 struct {
	Schema              int    `json:"schema"`
	OperationID         string `json:"operationId"`
	MarkerDigest        string `json:"markerDigest"`
	Verifier            string `json:"verifier"`
	EndpointID          string `json:"endpointId"`
	PrincipalID         string `json:"principalId"`
	AuthenticationClass string `json:"authenticationClass"`
	BinaryRevision      string `json:"binaryRevision"`
	ServiceRevision     string `json:"serviceRevision"`
	Configuration       string `json:"configurationRevision"`
	IssuedAt            int64  `json:"issuedAt"`
	IssuedAtMillis      int64  `json:"issuedAtMillis"`
	ExpiresAt           int64  `json:"expiresAt"`
	ProofedAt           int64  `json:"proofedAt,omitempty"`
	ConsumedAt          int64  `json:"consumedAt,omitempty"`
	EvidenceRevision    string `json:"evidenceRevision,omitempty"`
}

func NewHost(composition Composition, rollbackAuthority broker.CompletedMutationAuthority) (*Host, error) {
	registered, err := ResolveRegisteredSSHComposition(composition)
	if err != nil {
		return nil, err
	}
	return NewHostFromResolvedComposition(registered, rollbackAuthority)
}

// NewHostFromResolvedComposition constructs the broker host from the exact
// catalog record selected by the trusted composition root.
func NewHostFromResolvedComposition(registered ResolvedSSHComposition, rollbackAuthority broker.CompletedMutationAuthority) (*Host, error) {
	return newHostFromResolvedComposition(registered, rollbackAuthority, true)
}

// newReadOnlyHostFromResolvedComposition reuses the exact production
// composition without granting mutation or rollback authority. It exists only
// for SSH-owned posture and recovery observation.
func newReadOnlyHostFromResolvedComposition(registered ResolvedSSHComposition) (*Host, error) {
	return newHostFromResolvedComposition(registered, nil, false)
}

func newHostFromResolvedComposition(registered ResolvedSSHComposition, rollbackAuthority broker.CompletedMutationAuthority, requireRollback bool) (*Host, error) {
	if !registered.Valid() {
		return nil, errors.New("resolved SSH composition is invalid")
	}
	composition := registered.Composition()
	host := &Host{implementation: registered.Implementation(), stageAuthority: rollbackAuthority, now: time.Now}
	if requireRollback && rollbackAuthority == nil {
		return nil, broker.StartupFailure("ssh", "ssh.rollback", string(composition.Implementation), "authority_unavailable", errors.New("SSH stage recovery authority is unavailable"))
	}
	var err error
	switch composition.ServiceControl {
	case ServiceControlSystemd:
		host.service, err = newSystemdSSHServiceControl(registered.serviceTarget.systemdUnitList()...)
	case ServiceControlProcd:
		host.service, err = newProcdSSHServiceControl(registered.serviceTarget.procdService)
	}
	if err != nil {
		return nil, sshStartupFailure("service.supervision", string(composition.ServiceControl), err)
	}
	switch composition.LogEvidence {
	case LogEvidenceJournald:
		host.logs, err = newJournaldSSHLogEvidence(registered.logTarget.journaldUnitList()...)
	case LogEvidenceLogread:
		host.logs, err = newLogreadSSHLogEvidence()
	}
	if err != nil {
		return nil, sshStartupFailure("ssh.security_log_evidence", string(composition.LogEvidence), err)
	}
	if composition.Implementation == ImplementationDropbear {
		dropbear, err := newDropbearUCIHost(host.service)
		if err != nil {
			return nil, sshStartupFailure("ssh.management", string(composition.Implementation), err)
		}
		if rollbackAuthority != nil {
			dropbear.rollbackAuthority = rollbackAuthority
		}
		host.dropbear = dropbear
		return host, nil
	}
	host.sshd, err = firstFixedBinary("/usr/sbin/sshd", "/usr/bin/sshd")
	if err != nil {
		return nil, sshStartupFailure("ssh.management", string(composition.Implementation), err)
	}
	return host, nil
}

func RegisterHandlers(registry *broker.Registry, composition Composition, rollbackAuthority broker.CompletedMutationAuthority) error {
	host, err := NewHost(composition, rollbackAuthority)
	if err != nil {
		return err
	}
	return registerHostHandlers(registry, host)
}

// RegisterHandlersFromResolved binds broker handlers to the same resolved
// composition record that is projected into Server Protection recovery.
func RegisterHandlersFromResolved(registry *broker.Registry, composition ResolvedSSHComposition, rollbackAuthority broker.CompletedMutationAuthority) error {
	host, err := NewHostFromResolvedComposition(composition, rollbackAuthority)
	if err != nil {
		return err
	}
	return registerHostHandlers(registry, host)
}

func registerHostHandlers(registry *broker.Registry, host *Host) error {
	definitions := []struct {
		verb     broker.Verb
		role     broker.Role
		mutation bool
		handler  broker.Handler
	}{
		{verb: broker.VerbSSHObserve, role: broker.RolePanel, handler: host.observeHandler},
		{verb: broker.VerbSSHPrepare, role: broker.RolePanel, handler: host.prepareHandler},
		{verb: broker.VerbSSHStage, role: broker.RolePanel, mutation: true, handler: host.stageHandler},
		{verb: broker.VerbSSHRecoverStage, role: broker.RolePanel, handler: host.recoverStageHandler},
		{verb: broker.VerbSSHReleaseStage, role: broker.RolePanel, mutation: true, handler: host.releaseStageHandler},
		{verb: broker.VerbSSHValidate, role: broker.RolePanel, mutation: true, handler: host.validateHandler},
		{verb: broker.VerbSSHReload, role: broker.RolePanel, mutation: true, handler: host.reloadHandler},
		{verb: broker.VerbSSHArm, role: broker.RolePanel, mutation: true, handler: host.armHandler},
		{verb: broker.VerbSSHRestore, role: broker.RolePanel, mutation: true, handler: host.restoreHandler},
		{verb: broker.VerbSSHInspect, role: broker.RolePanel, handler: host.inspectHandler},
		{verb: broker.VerbSSHVerify, role: broker.RolePanel, mutation: true, handler: host.verifyHandler},
		{verb: broker.VerbSSHProof, role: broker.RoleSSHProof, mutation: true, handler: host.proofHandler},
	}
	for _, value := range definitions {
		lifecycle := completedMutationLifecycle(host.implementation, value.verb)
		if err := registry.Register(value.verb, broker.Definition{Role: value.role, Mutation: value.mutation,
			RetainResultUntilRelease: lifecycle.RetainUntilRelease, ReleasesRetainedResultVerb: lifecycle.ReleasesVerb, Handler: value.handler}); err != nil {
			return broker.StartupFailure("ssh", "ssh.management", string(host.implementation), "handler_registration_failed", err)
		}
	}
	return nil
}

func (h *Host) prepareHandler(_ context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request PrepareRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || request.Policy.Validate() != nil {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH desired policy is invalid")
	}
	if h.implementation == ImplementationDropbear {
		prepared, _, err := prepareDropbearUCIPolicy(request.Policy)
		if err != nil {
			return nil, broker.Failure(broker.CodeValidation, "SSH policy is not representable by Dropbear")
		}
		return prepared, nil
	}
	prepared, err := prepareOpenSSHPolicy(request.Policy)
	if err != nil {
		return nil, broker.Failure(broker.CodeValidation, "SSH policy is not representable by OpenSSH")
	}
	return prepared, nil
}

func (h *Host) observeHandler(ctx context.Context, request broker.Request, _ broker.PeerIdentity) (result any, err error) {
	if err := decodeEmpty(request); err != nil {
		return nil, err
	}
	defer func() {
		if recover() != nil {
			result = nil
			err = listenerAuthorityWrap(broker.Failure(broker.CodeInternal, "broker handler failed"), "unexpected_failure", "authority_validate", "", "")
		}
	}()
	result, err = h.observe(ctx)
	if err != nil {
		return nil, ensureSSHObserveDiagnostic(err)
	}
	return result, nil
}

func (h *Host) stageHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request StageRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || request.Policy.Validate() != nil ||
		!digest(request.ExpectedArtifactDigest) || !safeToken(request.EndpointID, 256) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH managed policy payload is invalid")
	}
	if h.implementation == ImplementationDropbear {
		return h.dropbear.stage(ctx, envelope, request)
	}
	prepared, err := prepareOpenSSHPolicy(request.Policy)
	if err != nil || prepared.ArtifactDigest != request.ExpectedArtifactDigest {
		return nil, broker.Failure(broker.CodeValidation, "OpenSSH staged representation differs from preview")
	}
	managedContent := []byte(prepared.Representation)
	before, err := h.observe(ctx)
	if err != nil || checkExpected(envelope, before.Posture) != nil || !endpointExists(before.Posture, request.EndpointID) {
		return nil, broker.Failure(broker.CodeRevision, "SSH host revision changed before staging")
	}
	prior, err := inspectArtifact(true)
	if err != nil {
		return nil, broker.Failure(broker.CodeValidation, "SSH managed drop-in cannot be inspected safely")
	}
	if err := writeArtifact(managedContent, 0o600); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "SSH managed drop-in could not be staged")
	}
	after, err := h.observe(ctx)
	if err != nil {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "SSH staged state requires recovery")
	}
	return StageResultV1{ArtifactDigest: prepared.ArtifactDigest, EndpointID: request.EndpointID, Prior: prior,
		ProviderRevision: ProviderRevision, ConfigurationRevision: after.Posture.ConfigurationRevision}, nil
}

func (h *Host) recoverStageHandler(_ context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request RecoverStageRequestV1
	if broker.DecodePayload(envelope.Payload, &request) != nil || !safeToken(request.EndpointID, 256) || !digest(request.ExpectedArtifactDigest) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH completed stage recovery identity is invalid")
	}
	return authoritativeStageResult(h.stageAuthority, envelope.OperationID, request.EndpointID, request.ExpectedArtifactDigest, h.implementation)
}

func (h *Host) releaseStageHandler(_ context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request ReleaseStageRequestV1
	if broker.DecodePayload(envelope.Payload, &request) != nil || !safeToken(request.EndpointID, 256) || !digest(request.ExpectedArtifactDigest) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH completed stage release identity is invalid")
	}
	if _, err := authoritativeStageResult(h.stageAuthority, envelope.OperationID, request.EndpointID, request.ExpectedArtifactDigest, h.implementation); err != nil {
		return nil, err
	}
	return EmptyV1{}, nil
}

func (h *Host) validateHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request ValidationRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !digest(request.ArtifactDigest) || !safeToken(request.EndpointID, 256) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH validation identity is invalid")
	}
	if h.implementation == ImplementationDropbear {
		return h.dropbear.validate(ctx, envelope, request)
	}
	observation, err := h.observe(ctx)
	if err != nil || checkExpected(envelope, observation.Posture) != nil || !endpointExists(observation.Posture, request.EndpointID) {
		return nil, broker.Failure(broker.CodeRevision, "SSH host revision changed before validation")
	}
	artifact, err := inspectArtifact(false)
	if err != nil || !artifact.Present || artifact.Digest != request.ArtifactDigest {
		return nil, broker.Failure(broker.CodeRevision, "SSH staged artifact changed before validation")
	}
	if _, err := h.run(ctx, h.sshd, "-t", "-f", MainConfig); err != nil {
		return ValidationResultV1{ProviderRevision: ProviderRevision, ReasonCodes: []domain.ReasonCode{domain.ReasonConfigurationMismatch}}, nil
	}
	effective, err := h.run(ctx, h.sshd, "-T", "-f", MainConfig)
	if err != nil {
		return ValidationResultV1{SyntaxValid: true, ProviderRevision: ProviderRevision,
			ReasonCodes: []domain.ReasonCode{domain.ReasonConfigurationMismatch}}, nil
	}
	return ValidationResultV1{SyntaxValid: true, EffectiveValid: true, EffectiveRevision: domain.Revision(string(effective)),
		ProviderRevision: ProviderRevision}, nil
}

func (h *Host) reloadHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request ReloadRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !digest(request.ArtifactDigest) || !safeToken(request.EndpointID, 256) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH reload identity is invalid")
	}
	if h.implementation == ImplementationDropbear {
		return h.dropbear.reload(ctx, envelope, request)
	}
	before, err := h.observe(ctx)
	if err != nil || checkExpected(envelope, before.Posture) != nil || !endpointExists(before.Posture, request.EndpointID) {
		return nil, broker.Failure(broker.CodeRevision, "SSH host revision changed before reload")
	}
	artifact, err := inspectArtifact(false)
	if err != nil || artifact.Digest != request.ArtifactDigest {
		return nil, broker.Failure(broker.CodeRevision, "SSH staged artifact changed before reload")
	}
	if err := h.service.Reload(ctx); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "SSH service reload failed")
	}
	after, err := h.observe(ctx)
	if err != nil || after.Posture.ConfigurationRevision != before.Posture.ConfigurationRevision {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "SSH reload verification requires recovery")
	}
	return ReloadResultV1{ServiceRevision: after.Posture.ServiceRevision,
		ConfigurationRevision: after.Posture.ConfigurationRevision, ProviderRevision: ProviderRevision}, nil
}

func (h *Host) armHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request ArmRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !digest(request.MarkerDigest) ||
		!safeToken(request.Verifier, 128) || !safeToken(request.EndpointID, 256) || !safeToken(request.PrincipalID, 256) ||
		request.AuthenticationClass != "publickey" && request.AuthenticationClass != "certificate" || !ValidExpiry(request.ExpiresAt, h.time()) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH reconnect proof ticket is invalid")
	}
	observation, err := h.observe(ctx)
	if err != nil || checkExpected(envelope, observation.Posture) != nil || !endpointExists(observation.Posture, request.EndpointID) {
		return nil, broker.Failure(broker.CodeRevision, "SSH reconnect proof host identity changed")
	}
	if err := ensureTicketRoot(); err != nil {
		return nil, broker.Failure(broker.CodeInternal, "SSH proof journal is unavailable")
	}
	issuedAt := h.time()
	ticket := proofTicketV1{Schema: 2, OperationID: envelope.OperationID, MarkerDigest: request.MarkerDigest, Verifier: request.Verifier,
		EndpointID: request.EndpointID, PrincipalID: request.PrincipalID, AuthenticationClass: request.AuthenticationClass,
		BinaryRevision: observation.Posture.BinaryRevision, ServiceRevision: observation.Posture.ServiceRevision,
		Configuration: observation.Posture.ConfigurationRevision, IssuedAt: issuedAt.Unix(), IssuedAtMillis: issuedAt.UnixMilli(), ExpiresAt: request.ExpiresAt}
	if err := writeTicket(ticket); err != nil {
		return nil, broker.Failure(broker.CodeInternal, "SSH proof ticket could not be persisted")
	}
	return EmptyV1{}, nil
}

func (h *Host) restoreHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request RestoreRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !digest(request.ExpectedCurrentArtifactDigest) || !basicPrior(request.Prior) || !safeToken(request.EndpointID, 256) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH rollback checkpoint is invalid")
	}
	if h.implementation == ImplementationDropbear {
		return h.dropbear.restore(ctx, envelope, request)
	}
	staged, err := authoritativeStageResult(h.stageAuthority, envelope.OperationID, request.EndpointID, request.ExpectedCurrentArtifactDigest, ImplementationOpenSSH)
	if err != nil {
		return nil, err
	}
	if !samePriorArtifact(request.Prior, staged.Prior) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH rollback checkpoint differs from broker authority")
	}
	if !validPrior(request.Prior) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH rollback checkpoint is invalid")
	}
	current, err := h.observe(ctx)
	if err != nil || checkExpected(envelope, current.Posture) != nil || !endpointExists(current.Posture, request.EndpointID) {
		return nil, broker.Failure(broker.CodeRevision, "SSH host revision changed before rollback")
	}
	artifact, err := inspectArtifact(false)
	if err != nil || artifact.Digest != request.ExpectedCurrentArtifactDigest {
		return nil, broker.Failure(broker.CodeFence, "SSH rollback would overwrite foreign state")
	}
	if request.Prior.Present {
		if err := writeArtifact(request.Prior.Content, os.FileMode(request.Prior.Mode)); err != nil {
			return nil, broker.Failure(broker.CodeRecoveryRequired, "SSH rollback restore failed")
		}
	} else if err := removeArtifact(); err != nil {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "SSH rollback remove failed")
	}
	after, err := h.observe(ctx)
	if err != nil {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "SSH rollback state cannot be verified")
	}
	restored, err := inspectArtifact(false)
	if err != nil || restored.Digest != request.Prior.Digest || restored.Present != request.Prior.Present || restored.Mode != request.Prior.Mode {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "SSH rollback exact-state verification failed")
	}
	return RestoreResultV1{ArtifactDigest: restored.Digest, ConfigurationRevision: after.Posture.ConfigurationRevision,
		ProviderRevision: ProviderRevision}, nil
}

func (h *Host) inspectHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	if h.implementation == ImplementationDropbear {
		var request InspectRequestV1
		if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !safeToken(request.EndpointID, 256) {
			return nil, broker.Failure(broker.CodeInvalidRequest, "SSH inspection target is invalid")
		}
		return h.dropbear.inspect(ctx, envelope, request)
	}
	var request InspectRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !safeToken(request.EndpointID, 256) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH inspection target is invalid")
	}
	observation, err := h.observe(ctx)
	if err != nil || checkExpected(envelope, observation.Posture) != nil || !endpointExists(observation.Posture, request.EndpointID) {
		return nil, broker.Failure(broker.CodeRevision, "SSH host revision changed before inspection")
	}
	artifact, err := inspectArtifact(false)
	if err != nil {
		return nil, broker.Failure(broker.CodeValidation, "SSH managed drop-in cannot be inspected safely")
	}
	return InspectResultV1{Present: artifact.Present, ArtifactDigest: artifact.Digest, Owner: artifact.Owner,
		Group: artifact.Group, ModeClass: artifact.ModeClass, Mode: artifact.Mode, ConfigurationRevision: observation.Posture.ConfigurationRevision}, nil
}

func (h *Host) proofHandler(ctx context.Context, envelope broker.Request, peer broker.PeerIdentity) (any, error) {
	if err := decodeEmpty(envelope); err != nil {
		return nil, err
	}
	session, err := h.proofSession(ctx, peer)
	if err != nil {
		return nil, broker.Failure(broker.CodeUnauthorized, "fresh SSH public-key session could not be proven")
	}
	tickets, err := readTickets()
	if err != nil {
		return nil, broker.Failure(broker.CodeInternal, "SSH proof journal is unavailable")
	}
	now := h.time()
	observation, err := h.observe(ctx)
	if err != nil {
		return nil, broker.Failure(broker.CodeRevision, "SSH host changed after the proof ticket was armed")
	}
	matches := make([]proofTicketV1, 0, 1)
	for _, ticket := range tickets {
		if ticket.ProofedAt == 0 && ticket.ConsumedAt == 0 && ticket.IssuedAtMillis < session.authenticatedAtMillis && ticket.ExpiresAt > now.Unix() &&
			ticket.PrincipalID == session.principalID && ticket.AuthenticationClass == session.authenticationClass &&
			endpointMatchesSession(observation.Posture, ticket.EndpointID, session.local) {
			matches = append(matches, ticket)
		}
	}
	if len(matches) != 1 {
		return nil, broker.Failure(broker.CodeValidation, "fresh SSH session does not select exactly one proof ticket")
	}
	ticket := matches[0]
	if observation.Posture.BinaryRevision != ticket.BinaryRevision || observation.Posture.ServiceRevision != ticket.ServiceRevision ||
		observation.Posture.ConfigurationRevision != ticket.Configuration {
		return nil, broker.Failure(broker.CodeRevision, "SSH host changed after the proof ticket was armed")
	}
	ticket.ProofedAt = now.Unix()
	ticket.EvidenceRevision = domain.Revision(struct {
		Operation, Marker, Principal, Endpoint, Source, Peer string
		At                                                   int64
	}{ticket.OperationID, ticket.MarkerDigest, ticket.PrincipalID, ticket.EndpointID, session.sourcePrefix, peer.Revision, session.authenticatedAtMillis})
	if err := writeTicket(ticket); err != nil {
		return nil, broker.Failure(broker.CodeInternal, "SSH proof evidence could not be persisted")
	}
	return ProofResultV1{OperationID: ticket.OperationID, Verifier: ticket.Verifier, ExpiresAt: ticket.ExpiresAt}, nil
}

func (h *Host) verifyHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request VerifyRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH proof verification payload is invalid")
	}
	ticket, err := readTicket(envelope.OperationID)
	if err != nil || ticket.ProofedAt == 0 || ticket.ConsumedAt != 0 || ticket.ExpiresAt <= h.time().Unix() ||
		ticket.MarkerDigest != request.MarkerDigest || ticket.Verifier != request.Verifier || ticket.EndpointID != request.EndpointID ||
		ticket.PrincipalID != request.PrincipalID || ticket.AuthenticationClass != request.AuthenticationClass {
		return VerifyResultV1{}, nil
	}
	observation, err := h.observe(ctx)
	if err != nil || checkExpected(envelope, observation.Posture) != nil || observation.Posture.BinaryRevision != ticket.BinaryRevision ||
		observation.Posture.ServiceRevision != ticket.ServiceRevision || observation.Posture.ConfigurationRevision != ticket.Configuration {
		return nil, broker.Failure(broker.CodeRevision, "SSH host changed before proof consumption")
	}
	ticket.ConsumedAt = h.time().Unix()
	if err := writeTicket(ticket); err != nil {
		return nil, broker.Failure(broker.CodeInternal, "SSH proof consumption could not be persisted")
	}
	return VerifyResultV1{Verified: true, Independent: true, FreshSession: true, OperationBound: true,
		EndpointID: ticket.EndpointID, PrincipalID: ticket.PrincipalID, AuthenticationClass: ticket.AuthenticationClass,
		EvidenceRevision: ticket.EvidenceRevision}, nil
}

func (h *Host) observe(ctx context.Context) (ObservationV1, error) {
	if h.observePosture != nil {
		return h.observePosture(ctx)
	}
	if h.implementation == ImplementationDropbear {
		return h.dropbear.observe(ctx, h.time())
	}
	now := h.time()
	if err := h.sshd.Revalidate(); err != nil {
		return ObservationV1{}, listenerAuthorityWrap(err, "binary_identity_unavailable", "process_fence", "", "")
	}
	binaryIdentity := h.sshd.Identity()
	binaryContentSHA256 := binaryIdentity.Digest
	if binaryContentSHA256 == "" {
		return ObservationV1{}, listenerAuthorityWrap(errors.New("selected sshd binary identity is unavailable"), "binary_identity_unavailable", "process_fence", "", "")
	}
	versionOutput, err := h.run(ctx, h.sshd, "-V")
	if err != nil && len(versionOutput) == 0 {
		return ObservationV1{}, listenerAuthorityWrap(err, "binary_identity_unavailable", "process_fence", "", "")
	}
	versionClass := opensshVersionClass(string(versionOutput))
	serviceObservation, err := h.service.Observe(ctx)
	if err != nil {
		return ObservationV1{}, listenerAuthorityWrap(err, "service_evidence_unavailable", "service_observe", "", "")
	}
	unit, serviceRevision := serviceObservation.ID, serviceObservation.Revision
	graph, err := configGraph()
	if err != nil {
		return ObservationV1{}, listenerAuthorityWrap(err, "configuration_evidence_unavailable", "configuration_read", "", "")
	}
	effectiveOutput, err := h.run(ctx, h.sshd, "-T", "-f", MainConfig)
	if err != nil {
		return ObservationV1{}, listenerAuthorityWrap(err, "configuration_evidence_unavailable", "configuration_read", "", "")
	}
	effective := parseEffective(effectiveOutput)
	configurationRevision := domain.Revision(struct {
		Graph     []domain.ConfigNodeV1
		Effective string
	}{graph, domain.Revision(string(effectiveOutput))})
	capabilities := domain.CapabilitySetV1{ObservePosture: domain.AvailabilityAvailable, Prepare: domain.AvailabilityAvailable,
		Stage: domain.AvailabilityAvailable, Validate: domain.AvailabilityAvailable, Reload: domain.AvailabilityAvailable,
		Reconnect: domain.AvailabilityAvailable, Rollback: domain.AvailabilityAvailable}
	capabilities.Revision = domain.Revision(capabilities)
	posture := domain.SSHPostureV1{Schema: domain.PostureSchemaV1,
		Binary:      domain.BinaryIdentityV1{Implementation: "openssh", VersionClass: versionClass, Digest: binaryContentSHA256, Selected: true},
		Service:     domain.ServiceIdentityV1{Manager: string(h.service.Kind()), UnitID: unit, State: "active", Digest: serviceRevision},
		ConfigGraph: graph, MatchContexts: []domain.MatchContextV1{{ID: "global", ConditionClass: "global", EffectiveHash: domain.Revision(string(effectiveOutput)), Known: true}},
		Authentication: authenticationPosture(effective), Forwarding: forwardingPosture(effective), AuthorizedKeys: authorizedKeysPosture(effective),
		HostKeys: hostKeyPosture(), Capabilities: capabilities, ObservedAt: now.Unix(), ExpiresAt: now.Add(domain.MaxPostureLifetime).Unix(),
		BinaryRevision: binaryContentSHA256, ServiceRevision: serviceRevision, ConfigurationRevision: configurationRevision}
	posture.Endpoints, err = endpoints(effective, configurationRevision, now)
	if err != nil {
		return ObservationV1{}, listenerAuthorityWrap(err, "configuration_evidence_invalid", "authority_validate", "", "")
	}
	posture.ListenerAuthorities, err = h.observeOpenSSHListeners(ctx, serviceObservation, posture.Endpoints, binaryIdentity, serviceRevision, configurationRevision, now)
	if err != nil {
		return ObservationV1{}, err
	}
	posture.SemanticRevision = domain.PostureSemanticRevision(posture)
	if err := posture.Validate(now); err != nil {
		return ObservationV1{}, listenerAuthorityWrap(err, "posture_invalid", "authority_validate", "", "")
	}
	return ObservationV1{Posture: posture, ProviderRevision: ProviderRevision}, nil
}

type openSSHSystemdEvidence struct {
	UnitID             string
	MainPID            int
	ControlGroup       string
	FragmentPath       string
	FragmentSHA256     string
	StartMonotonicUsec uint64
}

func (h *Host) observeOpenSSHListeners(ctx context.Context, beforeObservation sshServiceControlObservation, endpoints []hostresources.ManagementEndpointV1, binaryIdentity executableobject.Identity, serviceRevision, configurationRevision string, now time.Time) ([]domain.SSHListenerAuthorityV1, error) {
	binaryContentSHA256 := binaryIdentity.Digest
	if h.service.Kind() != ServiceControlSystemd {
		return nil, listenerAuthorityWrap(errors.New("OpenSSH listener authority requires its selected systemd supervisor"), "evidence_invalid", "authority_validate", "", "")
	}
	before, err := parseOpenSSHSystemdEvidence(beforeObservation)
	if err != nil {
		return nil, listenerAuthorityWrap(err, "evidence_invalid", "process_fence", "", "")
	}
	process, err := observeSSHProcess(ctx, before.MainPID, binaryIdentity)
	if err != nil || process.ControlGroup == "" || process.ControlGroup != before.ControlGroup {
		if err == nil {
			err = errors.New("OpenSSH process identity differs from its systemd service")
		}
		return nil, listenerAuthorityWrap(err, "process_identity_unavailable", "process_fence", "", "")
	}
	allowedPorts := make(map[uint16]bool, len(endpoints))
	for _, endpoint := range endpoints {
		allowedPorts[endpoint.Port] = true
	}
	observation, err := listenerevidence.ObserveAcceptingTCPDetailed(ctx, before.MainPID, allowedPorts)
	if err != nil {
		return nil, fmt.Errorf("OpenSSH accepting listener authority is unavailable: %w", err)
	}
	sockets := observation.Sockets
	if len(sockets) == 0 {
		return nil, listenerAuthorityFailure("socket_not_accepting", "configured_listener_match", observation.Diagnostic.ErrnoClass, observation.Diagnostic.ProofMethod)
	}
	afterObservation, err := h.service.Observe(ctx)
	if err != nil {
		return nil, listenerAuthorityWrap(err, "service_generation_changed", "stability_recheck", "", "")
	}
	after, err := parseOpenSSHSystemdEvidence(afterObservation)
	if err != nil || before != after {
		if err == nil {
			err = errors.New("OpenSSH systemd identity changed during listener observation")
		}
		return nil, listenerAuthorityWrap(err, "service_generation_changed", "stability_recheck", "", "")
	}
	fresh, err := processevidence.Observe(before.MainPID)
	if err != nil || fresh.StartTime != process.StartTime || !sameSSHExecutableObject(fresh, binaryIdentity) {
		if err == nil {
			err = errors.New("OpenSSH process changed during listener observation")
		}
		return nil, listenerAuthorityWrap(err, "process_generation_changed", "stability_recheck", "", "")
	}
	pid := before.MainPID
	service := hostfacts.ServiceFact{
		SupervisorRevision: domain.Revision(struct {
			Kind, Unit, Process string
		}{"systemd", before.UnitID, process.EvidenceRevision}),
		CgroupAvailability: "available", CgroupPolicy: "required",
		CgroupRevision: domain.Revision(struct {
			Provider, Availability, Policy, Path string
		}{processevidence.RevisionV2, "available", "required", before.ControlGroup}),
		SystemdUnit: before.UnitID, MainPID: &pid, FragmentPath: before.FragmentPath, FragmentSHA256: before.FragmentSHA256,
		ActiveState: "active", SubState: "running", ControlGroup: before.ControlGroup, StartMonotonicUsec: before.StartMonotonicUsec,
	}
	authorities := make([]domain.SSHListenerAuthorityV1, 0, len(sockets))
	for _, socket := range sockets {
		ids := matchingConfiguredSSHEndpoints(socket, endpoints)
		if len(ids) == 0 {
			continue
		}
		authority := domain.SSHListenerAuthorityV1{
			Schema: domain.ListenerAuthoritySchemaV1, EndpointIDs: ids, InstanceID: before.UnitID,
			Socket: socket, Process: process, Service: service, BinaryRevision: binaryContentSHA256,
			ServiceRevision: serviceRevision, ConfigurationRevision: configurationRevision,
			ObservedAt: now.Unix(), ExpiresAt: now.Add(domain.MaxListenerAuthorityLifetime).Unix(),
		}
		authority.Seal()
		authorities = append(authorities, authority)
	}
	if len(authorities) == 0 {
		return nil, listenerAuthorityFailure("socket_identity_mismatch", "configured_endpoint_match", observation.Diagnostic.ErrnoClass, observation.Diagnostic.ProofMethod)
	}
	return authorities, nil
}

func parseOpenSSHSystemdEvidence(observation sshServiceControlObservation) (openSSHSystemdEvidence, error) {
	if !safeToken(observation.ID, 128) || len(observation.Evidence) == 0 || len(observation.Evidence) > maxCommandOutput {
		return openSSHSystemdEvidence{}, errors.New("OpenSSH systemd evidence is malformed")
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(observation.Evidence), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || key == "" || values[key] != "" {
			continue
		}
		values[key] = value
	}
	pid, pidErr := strconv.Atoi(values["MainPID"])
	start, startErr := strconv.ParseUint(values["ExecMainStartTimestampMonotonic"], 10, 64)
	fragment := filepath.Clean(values["FragmentPath"])
	data, _, _, fragmentErr := secureRootFile(fragment, 1<<20)
	if values["Id"] != observation.ID || values["LoadState"] != "loaded" || values["ActiveState"] != "active" || values["SubState"] != "running" ||
		pidErr != nil || pid <= 1 || startErr != nil || start == 0 || values["ControlGroup"] == "" || !filepath.IsAbs(values["ControlGroup"]) ||
		fragmentErr != nil || !filepath.IsAbs(fragment) {
		return openSSHSystemdEvidence{}, errors.New("OpenSSH systemd service identity is unavailable")
	}
	return openSSHSystemdEvidence{UnitID: observation.ID, MainPID: pid, ControlGroup: values["ControlGroup"], FragmentPath: fragment,
		FragmentSHA256: rawFileContentSHA256(data), StartMonotonicUsec: start}, nil
}

func rawFileContentSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func matchingConfiguredSSHEndpoints(socket hostfacts.ListenerSocketIdentityV1, endpoints []hostresources.ManagementEndpointV1) []string {
	result := make([]string, 0, 2)
	for _, endpoint := range endpoints {
		if endpoint.Network != hostresources.NetworkTCP || endpoint.Port != socket.Port || !socketCoversEndpointFamily(socket, endpoint.Family) {
			continue
		}
		configured := hostresources.NormalizeListen(endpoint.Bind).Value
		if configured == socket.Bind || endpoint.Wildcard && socket.Wildcard {
			result = append(result, endpoint.ID)
		}
	}
	sort.Strings(result)
	return result
}

func socketCoversEndpointFamily(socket hostfacts.ListenerSocketIdentityV1, family hostresources.AddressFamily) bool {
	for _, covered := range socket.CoverageFamilies {
		if hostresources.AddressFamily(covered) == family {
			return true
		}
	}
	return false
}

func (h *Host) run(ctx context.Context, object sshExecutableObject, args ...string) ([]byte, error) {
	return runSSHCapabilityCommand(ctx, object, args...)
}

type boundedBuffer struct {
	data      bytes.Buffer
	truncated bool
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	length := len(value)
	remaining := maxCommandOutput - b.data.Len()
	if remaining <= 0 {
		b.truncated = b.truncated || length > 0
		return length, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.truncated = true
	}
	_, _ = b.data.Write(value)
	return length, nil
}

func checkExpected(request broker.Request, posture domain.SSHPostureV1) error {
	if request.Expected.Provider != "" && request.Expected.Provider != ProviderRevision ||
		request.Expected.Binary != "" && request.Expected.Binary != posture.BinaryRevision ||
		request.Expected.Service != "" && request.Expected.Service != posture.ServiceRevision ||
		request.Expected.Configuration != "" && request.Expected.Configuration != posture.ConfigurationRevision {
		return errors.New("SSH expected revision changed")
	}
	return nil
}

func decodeEmpty(request broker.Request) error {
	var empty EmptyV1
	if err := broker.DecodePayload(request.Payload, &empty); err != nil {
		return broker.Failure(broker.CodeInvalidRequest, "SSH broker payload is malformed")
	}
	return nil
}

func firstFixedBinary(paths ...string) (*executableobject.Object, error) {
	return firstFixedBinaryWithPolicy(executableobject.Policy{
		MaxBytes: 256 << 20, RequireRegular: true, RequireExecutable: true,
		RequireRootOwner: true, ForbiddenMode: 0o022,
		RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022,
	}, paths...)
}

func firstFixedBinaryWithPolicy(policy executableobject.Policy, paths ...string) (*executableobject.Object, error) {
	var selected *executableobject.Object
	for _, path := range paths {
		object, err := executableobject.Open(path, policy)
		if err != nil {
			continue
		}
		if selected == nil {
			selected = object
			continue
		}
		left, right := selected.Identity(), object.Identity()
		if left.Device != right.Device || left.Inode != right.Inode || left.Digest != right.Digest {
			_ = selected.Close()
			_ = object.Close()
			return nil, errRequiredFixedHostBinaryUnavailable
		}
		_ = object.Close()
	}
	if selected != nil {
		return selected, nil
	}
	return nil, errRequiredFixedHostBinaryUnavailable
}

func sshStartupFailure(capability, adapter string, err error) error {
	reason := "adapter_initialization_failed"
	switch {
	case errors.Is(err, errOpenWrtInitScriptAbsent):
		reason = "init_script_absent"
	case errors.Is(err, errUbusUnavailable):
		reason = "ubus_unavailable"
	case errors.Is(err, errDropbearBinaryUnavailable):
		reason = "dropbear_binary_absent"
	case errors.Is(err, errRequiredFixedHostBinaryUnavailable):
		reason = "required_executable_unavailable"
	case errors.Is(err, errOpenWrtInitScriptRejected), errors.Is(err, errControlOperationRejected):
		reason = "control_operation_rejected"
	case errors.Is(err, errProcdUnavailable):
		reason = "procd_unavailable"
	case errors.Is(err, errServiceNotRegistered):
		reason = "service_not_registered"
	case errors.Is(err, errServiceRegisteredStopped):
		reason = "service_registered_stopped"
	}
	return broker.StartupFailure("ssh", capability, adapter, reason, err)
}

func (h *Host) time() time.Time {
	if h.now != nil {
		return h.now().UTC().Truncate(time.Second)
	}
	return time.Now().UTC().Truncate(time.Second)
}

func opensshVersionClass(output string) string {
	for _, field := range strings.Fields(strings.TrimSpace(output)) {
		if strings.HasPrefix(field, "OpenSSH_") {
			value := strings.TrimPrefix(strings.Trim(field, ","), "OpenSSH_")
			value = strings.Map(func(r rune) rune {
				if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' {
					return r
				}
				return -1
			}, value)
			if value != "" && len(value) <= 48 {
				return "portable_" + strings.ReplaceAll(value, ".", "_")
			}
		}
	}
	return "portable_unknown"
}

func configGraph() ([]domain.ConfigNodeV1, error) {
	paths := []string{MainConfig}
	dropins, err := filepath.Glob("/etc/ssh/sshd_config.d/*.conf")
	if err != nil || len(dropins) > 63 {
		return nil, errors.New("SSH configuration graph is too large")
	}
	sort.Strings(dropins)
	paths = append(paths, dropins...)
	result := make([]domain.ConfigNodeV1, 0, len(paths))
	for index, path := range paths {
		data, info, stat, err := secureRootFile(path, MaxDropInBytes*8)
		if err != nil {
			return nil, err
		}
		kind, parent, depth := "include", "main", uint8(1)
		id := "config:" + domain.Revision(path)[:16]
		if index == 0 {
			kind, parent, depth, id = "main", "", 0, "main"
		} else if path == ManagedDropIn {
			kind, id = "managed_dropin", "managed-dropin"
		}
		result = append(result, domain.ConfigNodeV1{ID: id, ParentID: parent, Kind: kind, Order: uint16(index), Depth: depth,
			Digest: domain.Revision(data), Owner: "root", ModeClass: configModeClass(info.Mode().Perm()), Symlink: false})
		_ = stat
	}
	return result, nil
}

func secureRootFile(path string, limit int64) ([]byte, os.FileInfo, *syscall.Stat_t, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 0 || info.Size() > limit || info.Mode().Perm()&0o022 != 0 {
		return nil, nil, nil, errors.New("SSH file identity or mode is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return nil, nil, nil, errors.New("SSH file is not root-owned")
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) != info.Size() {
		return nil, nil, nil, errors.New("SSH file changed during read")
	}
	return data, info, stat, nil
}

func configModeClass(mode os.FileMode) string {
	if mode&0o200 != 0 {
		return "owner_read_write"
	}
	return "system_read"
}

func inspectArtifact(allowAbsent bool) (PriorArtifactV1, error) {
	info, err := os.Lstat(ManagedDropIn)
	if errors.Is(err, os.ErrNotExist) && allowAbsent || errors.Is(err, os.ErrNotExist) {
		return PriorArtifactV1{Owner: "root", Group: "root", ModeClass: "owner_read_write", Mode: 0o600, Digest: domain.Revision([]byte{})}, nil
	}
	if err != nil {
		return PriorArtifactV1{}, err
	}
	data, _, _, err := secureRootFile(ManagedDropIn, MaxDropInBytes)
	if err != nil {
		return PriorArtifactV1{}, err
	}
	return PriorArtifactV1{Present: true, Content: data, Owner: "root", Group: "root", ModeClass: configModeClass(info.Mode().Perm()),
		Mode: uint32(info.Mode().Perm()), Digest: domain.Revision(data)}, nil
}

func writeArtifact(content []byte, mode os.FileMode) error {
	if len(content) == 0 || len(content) > MaxDropInBytes || mode != 0o600 && mode != 0o640 && mode != 0o644 {
		return errors.New("SSH managed artifact content or mode is invalid")
	}
	directory := filepath.Dir(ManagedDropIn)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("SSH drop-in directory is unsafe")
	}
	temporary, err := os.CreateTemp(directory, ".90-solovey-ui.conf.stage-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	ok := false
	defer func() {
		_ = temporary.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if err := temporary.Chown(0, 0); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, ManagedDropIn); err != nil {
		return err
	}
	ok = true
	return syncDirectory(directory)
}

func removeArtifact() error {
	info, err := os.Lstat(ManagedDropIn)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("SSH managed artifact remove target is unsafe")
	}
	if err := os.Remove(ManagedDropIn); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(ManagedDropIn))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func validPrior(prior PriorArtifactV1) bool {
	if prior.Owner != "root" || prior.Group != "root" || prior.ModeClass != "owner_read_write" || prior.Mode != 0o600 && prior.Mode != 0o640 && prior.Mode != 0o644 || !digest(prior.Digest) {
		return false
	}
	if prior.Present {
		return len(prior.Content) > 0 && len(prior.Content) <= MaxDropInBytes && prior.Digest == domain.Revision(prior.Content)
	}
	return len(prior.Content) == 0 && prior.Digest == domain.Revision([]byte{})
}

func basicPrior(prior PriorArtifactV1) bool {
	return prior.Owner == "root" && prior.Group == "root" && prior.ModeClass == "owner_read_write" &&
		(prior.Mode == 0o600 || prior.Mode == 0o640 || prior.Mode == 0o644) && len(prior.Content) <= MaxCheckpointBytes && digest(prior.Digest)
}

func parseEffective(data []byte) map[string][]string {
	result := map[string][]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) >= 2 && len(fields[0]) <= 64 {
			key := strings.ToLower(fields[0])
			result[key] = append(result[key], strings.Join(fields[1:], " "))
		}
	}
	return result
}

func first(values map[string][]string, key, fallback string) string {
	if items := values[key]; len(items) > 0 && items[0] != "" {
		return strings.ToLower(items[0])
	}
	return fallback
}

func authenticationPosture(values map[string][]string) domain.AuthenticationPostureV1 {
	tries, _ := strconv.ParseUint(first(values, "maxauthtries", "6"), 10, 16)
	grace := parseSeconds(first(values, "logingracetime", "120"), 120)
	methods := strings.Fields(first(values, "authenticationmethods", "publickey"))
	if len(methods) == 1 && methods[0] == "any" {
		methods = []string{"publickey"}
	}
	return domain.AuthenticationPostureV1{PasswordAuthentication: first(values, "passwordauthentication", "yes"),
		KbdInteractiveAuthentication: first(values, "kbdinteractiveauthentication", "yes"), PermitRootLogin: first(values, "permitrootlogin", "prohibit-password"),
		PubkeyAuthentication: first(values, "pubkeyauthentication", "yes"), AuthenticationMethods: methods,
		MaxAuthTries: uint16(tries), LoginGraceTimeSeconds: uint32(grace), MaxStartupsClass: safeValue(first(values, "maxstartups", "10:30:100"), "bounded_default")}
}

func forwardingPosture(values map[string][]string) domain.ForwardingPostureV1 {
	return domain.ForwardingPostureV1{AllowAgentForwarding: first(values, "allowagentforwarding", "yes"),
		AllowTCPForwarding: first(values, "allowtcpforwarding", "yes"), GatewayPorts: first(values, "gatewayports", "no"),
		PermitTunnel: first(values, "permittunnel", "no"), X11Forwarding: first(values, "x11forwarding", "yes")}
}

func authorizedKeysPosture(values map[string][]string) domain.AuthorizedKeysPostureV1 {
	templates := strings.Fields(first(values, "authorizedkeysfile", ".ssh/authorized_keys"))
	return domain.AuthorizedKeysPostureV1{StrictModes: first(values, "strictmodes", "yes"), PathTemplateCount: uint16(len(templates)),
		PathTemplateRevision: domain.Revision(templates)}
}

func hostKeyPosture() []domain.HostKeyPostureV1 {
	paths, _ := filepath.Glob("/etc/ssh/ssh_host_*_key")
	sort.Strings(paths)
	result := make([]domain.HostKeyPostureV1, 0, len(paths))
	for _, path := range paths {
		data, info, _, err := secureRootFile(path, 64<<10)
		if err != nil {
			continue
		}
		kind := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "ssh_host_"), "_key")
		if !safeToken(kind, 64) {
			continue
		}
		result = append(result, domain.HostKeyPostureV1{Type: kind, Fingerprint: domain.Revision(data), Count: 1,
			Owner: "root", ModeClass: configModeClass(info.Mode().Perm())})
	}
	return result
}

func endpoints(values map[string][]string, configuration string, now time.Time) ([]hostresources.ManagementEndpointV1, error) {
	addresses := values["listenaddress"]
	if len(addresses) == 0 || len(addresses) > 64 {
		return nil, errors.New("OpenSSH effective listener configuration is unavailable")
	}
	seen := make(map[string]bool, len(addresses))
	result := make([]hostresources.ManagementEndpointV1, 0, len(addresses))
	for _, value := range addresses {
		fields := strings.Fields(value)
		if len(fields) != 1 && (len(fields) != 3 || !strings.EqualFold(fields[1], "rdomain") || !safeToken(fields[2], 64)) {
			return nil, errors.New("OpenSSH effective listener configuration is malformed")
		}
		listen, err := netip.ParseAddrPort(fields[0])
		if err != nil || listen.Port() == 0 || listen.Addr().Zone() != "" || listen.Addr().Is4In6() {
			return nil, errors.New("OpenSSH effective listener configuration is malformed")
		}
		address := listen.Addr()
		family := hostresources.AddressFamilyIPv6
		if address.Is4() {
			family = hostresources.AddressFamilyIPv4
		}
		bind := address.String()
		id := openSSHEndpointID(family, listen.Port(), bind)
		if seen[id] {
			return nil, errors.New("OpenSSH effective listener configuration is ambiguous")
		}
		seen[id] = true
		intent := hostresources.EndpointIntentPublic
		if address.IsLoopback() {
			intent = hostresources.EndpointIntentLocal
		}
		result = append(result, hostresources.ManagementEndpointV1{Schema: hostresources.ManagementEndpointSchemaV1,
			ID: id, Network: hostresources.NetworkTCP, Family: family, Bind: bind, Port: listen.Port(),
			ServiceKind: hostresources.ManagementSSH, Exposure: intent, Owner: "system", Purpose: "ssh_administrative_access",
			RecoveryPolicy: "fresh_independent_path_required", Source: "privileged_broker", ConfiguredIntent: true,
			Wildcard: bind == "0.0.0.0" || bind == "::", ConfidenceBP: 10000, ObservedAt: now.Unix(), ExpiresAt: now.Add(domain.MaxPostureLifetime).Unix(),
			ConfigurationRevision: configuration})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func endpointID(family hostresources.AddressFamily, port uint16) string {
	return fmt.Sprintf("management:ssh:configured:%s:%d", family, port)
}

func openSSHEndpointID(family hostresources.AddressFamily, port uint16, bind string) string {
	return fmt.Sprintf("management:ssh:configured:%s:%d:openssh:%s", family, port, domain.Revision(bind)[:16])
}

func endpointExists(posture domain.SSHPostureV1, id string) bool {
	for _, endpoint := range posture.Endpoints {
		if endpoint.ID == id {
			return true
		}
	}
	return false
}

func endpointMatchesSession(posture domain.SSHPostureV1, id string, local netip.AddrPort) bool {
	if !local.IsValid() || local.Port() == 0 {
		return false
	}
	for _, endpoint := range posture.Endpoints {
		if endpoint.ID != id || endpoint.Network != hostresources.NetworkTCP || endpoint.Port != local.Port() {
			continue
		}
		address := local.Addr().Unmap()
		if (endpoint.Family == hostresources.AddressFamilyIPv4) != address.Is4() {
			return false
		}
		bind, err := netip.ParseAddr(endpoint.Bind)
		if err != nil {
			return false
		}
		return bind.IsUnspecified() || bind.Unmap() == address
	}
	return false
}

func parseSeconds(value string, fallback uint64) uint64 {
	if parsed, err := strconv.ParseUint(value, 10, 32); err == nil {
		return parsed
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return fallback
	}
	return uint64(duration / time.Second)
}

func safeValue(value, fallback string) string {
	if safeToken(value, 64) {
		return value
	}
	return fallback
}

func safeToken(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

func digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

type sessionProof struct {
	principalID           string
	authenticationClass   string
	sourcePrefix          string
	local                 netip.AddrPort
	authenticatedAtMillis int64
}

func (h *Host) proofSession(ctx context.Context, peer broker.PeerIdentity) (sessionProof, error) {
	account, err := user.LookupId(strconv.FormatUint(uint64(peer.UID), 10))
	if err != nil || !safeToken(account.Username, 64) {
		return sessionProof{}, errors.New("SSH peer account is unavailable")
	}
	local, remote, daemonPID, err := sshSessionSocket(peer.PID, h.daemonName())
	if err != nil {
		return sessionProof{}, err
	}
	principalSum := sha256.Sum256([]byte("ssh:" + account.Username))
	principal := "principal:" + hex.EncodeToString(principalSum[:])
	observedAt, err := h.acceptedPublicKeyAt(ctx, account.Username, remote, daemonPID)
	if err != nil {
		return sessionProof{}, err
	}
	remoteAddress := remote.Addr().Unmap()
	bits := 128
	if remoteAddress.Is4() {
		bits = 32
	}
	return sessionProof{principalID: principal, authenticationClass: "publickey", sourcePrefix: netip.PrefixFrom(remoteAddress, bits).String(),
		local: local, authenticatedAtMillis: observedAt}, nil
}

func (h *Host) acceptedPublicKeyAt(ctx context.Context, username string, remote netip.AddrPort, daemonPID int) (int64, error) {
	if h.logs == nil {
		return 0, errors.New("SSH security-log evidence adapter is unavailable")
	}
	output, err := h.logs.Recent(ctx)
	if err != nil {
		return 0, err
	}
	if h.implementation == ImplementationDropbear && h.dropbear != nil {
		return h.dropbear.acceptedPublicKeyAt(output, username, remote, daemonPID, h.time())
	}
	if h.sshd == nil {
		return 0, errors.New("OpenSSH security-log parser is unavailable")
	}
	now := h.time()
	latest := int64(0)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64<<10), maxCommandOutput)
	for scanner.Scan() {
		var row struct {
			Message    string `json:"MESSAGE"`
			Identifier string `json:"SYSLOG_IDENTIFIER"`
			Executable string `json:"_EXE"`
			PID        string `json:"_PID"`
			Realtime   string `json:"__REALTIME_TIMESTAMP"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil || row.Identifier != "sshd" || row.Executable != h.sshd.Label() {
			continue
		}
		match := acceptedLogin.FindStringSubmatch(row.Message)
		address, addressErr := netip.ParseAddr(matchValue(match, 3))
		pid, pidErr := strconv.Atoi(row.PID)
		if match == nil || match[2] != username || addressErr != nil || address.Unmap() != remote.Addr().Unmap() || pidErr != nil || pid != daemonPID {
			continue
		}
		port, _ := strconv.ParseUint(match[4], 10, 16)
		micros, _ := strconv.ParseInt(row.Realtime, 10, 64)
		observedMillis := micros / 1_000
		if uint16(port) == remote.Port() && observedMillis > now.Add(-11*time.Minute).UnixMilli() &&
			observedMillis <= now.Add(5*time.Minute).UnixMilli() && observedMillis > latest {
			latest = observedMillis
		}
	}
	if latest == 0 {
		return 0, errors.New("fresh accepted public-key event is absent")
	}
	return latest, scanner.Err()
}

func matchValue(match []string, index int) string {
	if index < 0 || index >= len(match) {
		return ""
	}
	return match[index]
}

func (h *Host) daemonName() string {
	if h.implementation == ImplementationDropbear {
		return "dropbear"
	}
	return "sshd"
}

func sshSessionSocket(pid int, daemonName string) (netip.AddrPort, netip.AddrPort, int, error) {
	current := pid
	for depth := 0; depth < 12 && current > 1; depth++ {
		process, err := processevidence.Observe(current)
		if err != nil {
			break
		}
		if filepath.Base(process.Executable) == daemonName {
			if local, remote, err := processEstablishedSocket(current, process); err == nil {
				return local, remote, current, nil
			}
		}
		parent := process.ParentPID
		if parent <= 1 || parent == current {
			break
		}
		current = parent
	}
	return netip.AddrPort{}, netip.AddrPort{}, 0, errors.New("SSH session socket is unavailable")
}

type establishedSocket struct {
	inode  string
	local  netip.AddrPort
	remote netip.AddrPort
}

func processEstablishedSocket(pid int, process processevidence.Fact) (netip.AddrPort, netip.AddrPort, error) {
	before, err := readProcessEstablishedSockets(pid)
	if err != nil || len(before) != 1 {
		return netip.AddrPort{}, netip.AddrPort{}, errors.New("SSH session socket is unavailable or ambiguous")
	}
	middle, err := processevidence.Observe(pid)
	if err != nil || !sameProcessGeneration(process, middle) {
		return netip.AddrPort{}, netip.AddrPort{}, errors.New("SSH session process generation changed")
	}
	after, err := readProcessEstablishedSockets(pid)
	if err != nil || len(after) != 1 || after[0] != before[0] {
		return netip.AddrPort{}, netip.AddrPort{}, errors.New("SSH session socket changed while observing")
	}
	final, err := processevidence.Observe(pid)
	if err != nil || !sameProcessGeneration(process, final) {
		return netip.AddrPort{}, netip.AddrPort{}, errors.New("SSH session process generation changed")
	}
	return before[0].local, before[0].remote, nil
}

func readProcessEstablishedSockets(pid int) ([]establishedSocket, error) {
	inodes := map[string]bool{}
	descriptors, err := processevidence.ObserveDescriptorSnapshot(pid)
	if err != nil {
		return nil, errors.New("SSH session descriptors are unavailable")
	}
	for _, inode := range descriptors.SocketInodes {
		inodes[inode] = true
	}
	result := make([]establishedSocket, 0, 1)
	for _, table := range []struct {
		name string
		v6   bool
	}{{"tcp", false}, {"tcp6", true}} {
		data, err := readBoundedProcSocketTable(filepath.Join("/proc", strconv.Itoa(pid), "net", table.name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n")[1:] {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != "01" || !inodes[fields[9]] {
				continue
			}
			local, errA := parseProcAddress(fields[1], table.v6)
			remote, errB := parseProcAddress(fields[2], table.v6)
			if errA == nil && errB == nil && local.Port() != 0 && remote.Port() != 0 {
				result = append(result, establishedSocket{inode: fields[9], local: local, remote: remote})
				if len(result) > 1 {
					return nil, errors.New("SSH established socket is ambiguous")
				}
			}
		}
	}
	if len(result) != 1 {
		return nil, errors.New("SSH established socket is unavailable")
	}
	return result, nil
}

func readBoundedProcSocketTable(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxProcSocketTableBytes+1))
	if err != nil || int64(len(data)) > maxProcSocketTableBytes {
		return nil, errors.New("SSH process socket table exceeds its bound")
	}
	return data, nil
}

func sameProcessGeneration(left, right processevidence.Fact) bool {
	return left.PID == right.PID && left.StartTime == right.StartTime && left.UID == right.UID && left.GID == right.GID &&
		left.ExeDevice == right.ExeDevice && left.ExeInode == right.ExeInode && left.ExeDigest == right.ExeDigest
}

func parseProcAddress(value string, v6 bool) (netip.AddrPort, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return netip.AddrPort{}, errors.New("proc socket address is malformed")
	}
	port, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return netip.AddrPort{}, err
	}
	raw, err := hex.DecodeString(parts[0])
	if err != nil || !v6 && len(raw) != 4 || v6 && len(raw) != 16 {
		return netip.AddrPort{}, errors.New("proc socket IP is malformed")
	}
	for offset := 0; offset < len(raw); offset += 4 {
		raw[offset], raw[offset+3] = raw[offset+3], raw[offset]
		raw[offset+1], raw[offset+2] = raw[offset+2], raw[offset+1]
	}
	address, ok := netip.AddrFromSlice(raw)
	if !ok {
		return netip.AddrPort{}, errors.New("proc socket IP is invalid")
	}
	return netip.AddrPortFrom(address.Unmap(), uint16(port)), nil
}

var _ io.Writer = (*boundedBuffer)(nil)
