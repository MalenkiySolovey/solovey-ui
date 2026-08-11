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
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

const maxCommandOutput = 256 << 10

var acceptedLogin = regexp.MustCompile(`^Accepted (publickey) for ([A-Za-z0-9._-]{1,64}) from ([0-9A-Fa-f:.]{2,64}) port ([0-9]{1,5})`)

type Host struct {
	sshd       string
	systemctl  string
	journalctl string
	now        func() time.Time
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
	ExpiresAt           int64  `json:"expiresAt"`
	ProofedAt           int64  `json:"proofedAt,omitempty"`
	ConsumedAt          int64  `json:"consumedAt,omitempty"`
	EvidenceRevision    string `json:"evidenceRevision,omitempty"`
}

func NewHost() (*Host, error) {
	sshd, err := firstFixedBinary("/usr/sbin/sshd", "/usr/bin/sshd")
	if err != nil {
		return nil, err
	}
	systemctl, err := firstFixedBinary("/usr/bin/systemctl", "/bin/systemctl")
	if err != nil {
		return nil, err
	}
	journalctl, err := firstFixedBinary("/usr/bin/journalctl", "/bin/journalctl")
	if err != nil {
		return nil, err
	}
	return &Host{sshd: sshd, systemctl: systemctl, journalctl: journalctl, now: time.Now}, nil
}

func RegisterHandlers(registry *broker.Registry) error {
	host, err := NewHost()
	if err != nil {
		return err
	}
	definitions := []struct {
		verb     broker.Verb
		role     broker.Role
		mutation bool
		handler  broker.Handler
	}{
		{broker.VerbSSHObserve, broker.RolePanel, false, host.observeHandler},
		{broker.VerbSSHStage, broker.RolePanel, true, host.stageHandler},
		{broker.VerbSSHValidate, broker.RolePanel, true, host.validateHandler},
		{broker.VerbSSHReload, broker.RolePanel, true, host.reloadHandler},
		{broker.VerbSSHArm, broker.RolePanel, true, host.armHandler},
		{broker.VerbSSHRestore, broker.RolePanel, true, host.restoreHandler},
		{broker.VerbSSHInspect, broker.RolePanel, false, host.inspectHandler},
		{broker.VerbSSHVerify, broker.RolePanel, true, host.verifyHandler},
		{broker.VerbSSHProof, broker.RoleSSHProof, true, host.proofHandler},
	}
	for _, value := range definitions {
		if err := registry.Register(value.verb, broker.Definition{Role: value.role, Mutation: value.mutation, Handler: value.handler}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) observeHandler(ctx context.Context, request broker.Request, _ broker.PeerIdentity) (any, error) {
	if err := decodeEmpty(request); err != nil {
		return nil, err
	}
	return h.observe(ctx)
}

func (h *Host) stageHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request StageRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || len(request.ManagedContent) == 0 || len(request.ManagedContent) > MaxDropInBytes || bytes.IndexByte(request.ManagedContent, 0) >= 0 {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH managed drop-in payload is invalid")
	}
	before, err := h.observe(ctx)
	if err != nil || checkExpected(envelope, before.Posture) != nil {
		return nil, broker.Failure(broker.CodeRevision, "SSH host revision changed before staging")
	}
	prior, err := inspectArtifact(true)
	if err != nil {
		return nil, broker.Failure(broker.CodeValidation, "SSH managed drop-in cannot be inspected safely")
	}
	if err := writeArtifact(request.ManagedContent, 0o600); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "SSH managed drop-in could not be staged")
	}
	after, err := h.observe(ctx)
	if err != nil {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "SSH staged state requires recovery")
	}
	return StageResultV1{ArtifactDigest: domain.Revision(request.ManagedContent), Prior: prior,
		ProviderRevision: ProviderRevision, ConfigurationRevision: after.Posture.ConfigurationRevision}, nil
}

func (h *Host) validateHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request ValidationRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !digest(request.ArtifactDigest) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH validation identity is invalid")
	}
	observation, err := h.observe(ctx)
	if err != nil || checkExpected(envelope, observation.Posture) != nil {
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
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !digest(request.ArtifactDigest) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH reload identity is invalid")
	}
	before, err := h.observe(ctx)
	if err != nil || checkExpected(envelope, before.Posture) != nil {
		return nil, broker.Failure(broker.CodeRevision, "SSH host revision changed before reload")
	}
	artifact, err := inspectArtifact(false)
	if err != nil || artifact.Digest != request.ArtifactDigest {
		return nil, broker.Failure(broker.CodeRevision, "SSH staged artifact changed before reload")
	}
	unit := before.Posture.Service.UnitID
	if _, err := h.run(ctx, h.systemctl, "reload", unit); err != nil {
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
	ticket := proofTicketV1{Schema: 1, OperationID: envelope.OperationID, MarkerDigest: request.MarkerDigest, Verifier: request.Verifier,
		EndpointID: request.EndpointID, PrincipalID: request.PrincipalID, AuthenticationClass: request.AuthenticationClass,
		BinaryRevision: observation.Posture.BinaryRevision, ServiceRevision: observation.Posture.ServiceRevision,
		Configuration: observation.Posture.ConfigurationRevision, IssuedAt: h.time().Unix(), ExpiresAt: request.ExpiresAt}
	if err := writeTicket(ticket); err != nil {
		return nil, broker.Failure(broker.CodeInternal, "SSH proof ticket could not be persisted")
	}
	return EmptyV1{}, nil
}

func (h *Host) restoreHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request RestoreRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !digest(request.ExpectedCurrentArtifactDigest) || !validPrior(request.Prior) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "SSH rollback checkpoint is invalid")
	}
	current, err := h.observe(ctx)
	if err != nil || checkExpected(envelope, current.Posture) != nil {
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
	if err := decodeEmpty(envelope); err != nil {
		return nil, err
	}
	observation, err := h.observe(ctx)
	if err != nil || checkExpected(envelope, observation.Posture) != nil {
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
	matches := make([]proofTicketV1, 0, 1)
	for _, ticket := range tickets {
		if ticket.ProofedAt == 0 && ticket.ConsumedAt == 0 && ticket.IssuedAt < session.authenticatedAt && ticket.ExpiresAt > now.Unix() &&
			ticket.PrincipalID == session.principalID && ticket.AuthenticationClass == session.authenticationClass &&
			endpointMatches(ticket.EndpointID, session.family, session.localPort) {
			matches = append(matches, ticket)
		}
	}
	if len(matches) != 1 {
		return nil, broker.Failure(broker.CodeValidation, "fresh SSH session does not select exactly one proof ticket")
	}
	ticket := matches[0]
	observation, err := h.observe(ctx)
	if err != nil || observation.Posture.BinaryRevision != ticket.BinaryRevision || observation.Posture.ServiceRevision != ticket.ServiceRevision ||
		observation.Posture.ConfigurationRevision != ticket.Configuration {
		return nil, broker.Failure(broker.CodeRevision, "SSH host changed after the proof ticket was armed")
	}
	ticket.ProofedAt = now.Unix()
	ticket.EvidenceRevision = domain.Revision(struct {
		Operation, Marker, Principal, Endpoint, Source, Peer string
		At                                                   int64
	}{ticket.OperationID, ticket.MarkerDigest, ticket.PrincipalID, ticket.EndpointID, session.sourcePrefix, peer.Revision, session.authenticatedAt})
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
	now := h.time()
	binaryData, err := os.ReadFile(h.sshd)
	if err != nil || len(binaryData) == 0 || len(binaryData) > 256<<20 {
		return ObservationV1{}, errors.New("selected sshd binary is unavailable")
	}
	binaryRevision := domain.Revision(binaryData)
	versionOutput, err := h.run(ctx, h.sshd, "-V")
	if err != nil && len(versionOutput) == 0 {
		return ObservationV1{}, err
	}
	versionClass := opensshVersionClass(string(versionOutput))
	unit, serviceRevision, err := h.serviceIdentity(ctx)
	if err != nil {
		return ObservationV1{}, err
	}
	graph, err := configGraph()
	if err != nil {
		return ObservationV1{}, err
	}
	effectiveOutput, err := h.run(ctx, h.sshd, "-T", "-f", MainConfig)
	if err != nil {
		return ObservationV1{}, err
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
		Binary:      domain.BinaryIdentityV1{Implementation: "openssh", VersionClass: versionClass, Digest: binaryRevision, Selected: true},
		Service:     domain.ServiceIdentityV1{Manager: "systemd", UnitID: unit, State: "active", Digest: serviceRevision},
		ConfigGraph: graph, MatchContexts: []domain.MatchContextV1{{ID: "global", ConditionClass: "global", EffectiveHash: domain.Revision(string(effectiveOutput)), Known: true}},
		Authentication: authenticationPosture(effective), Forwarding: forwardingPosture(effective), AuthorizedKeys: authorizedKeysPosture(effective),
		HostKeys: hostKeyPosture(), Capabilities: capabilities, ObservedAt: now.Unix(), ExpiresAt: now.Add(domain.MaxPostureLifetime).Unix(),
		BinaryRevision: binaryRevision, ServiceRevision: serviceRevision, ConfigurationRevision: configurationRevision}
	posture.Endpoints = endpoints(effective, configurationRevision, now)
	posture.SemanticRevision = domain.PostureSemanticRevision(posture)
	if err := posture.Validate(now); err != nil {
		return ObservationV1{}, err
	}
	return ObservationV1{Posture: posture, ProviderRevision: ProviderRevision}, nil
}

func (h *Host) serviceIdentity(ctx context.Context) (string, string, error) {
	for _, unit := range []string{"ssh.service", "sshd.service"} {
		active, err := h.run(ctx, h.systemctl, "is-active", unit)
		if err != nil || strings.TrimSpace(string(active)) != "active" {
			continue
		}
		properties, err := h.run(ctx, h.systemctl, "show", unit, "--property=Id,LoadState,ActiveState,FragmentPath")
		if err != nil || !bytes.Contains(properties, []byte("LoadState=loaded")) || !bytes.Contains(properties, []byte("ActiveState=active")) {
			continue
		}
		return unit, domain.Revision(string(properties)), nil
	}
	return "", "", errors.New("selected SSH systemd service is not active")
}

func (h *Host) run(ctx context.Context, path string, args ...string) ([]byte, error) {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, path, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	var output boundedBuffer
	command.Stdout, command.Stderr = &output, &output
	err := command.Run()
	if output.truncated || bounded.Err() != nil {
		return output.data.Bytes(), errors.New("fixed SSH command exceeded its bound")
	}
	return output.data.Bytes(), err
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

func firstFixedBinary(paths ...string) (string, error) {
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}
	}
	return "", errors.New("required fixed host binary is unavailable")
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

func endpoints(values map[string][]string, configuration string, now time.Time) []hostresources.ManagementEndpointV1 {
	port64, _ := strconv.ParseUint(strings.Fields(first(values, "port", "22"))[0], 10, 16)
	port := uint16(port64)
	addresses := values["listenaddress"]
	if len(addresses) == 0 {
		addresses = []string{"0.0.0.0", "::"}
	}
	families := map[hostresources.AddressFamily]string{}
	for _, value := range addresses {
		addressText := strings.Fields(value)[0]
		addressText = strings.Trim(addressText, "[]")
		address, err := netip.ParseAddr(addressText)
		if err != nil {
			continue
		}
		family := hostresources.AddressFamilyIPv6
		bind := "::"
		if address.Is4() {
			family, bind = hostresources.AddressFamilyIPv4, "0.0.0.0"
		}
		if existing, ok := families[family]; !ok || existing == bind {
			families[family] = address.String()
		}
	}
	result := make([]hostresources.ManagementEndpointV1, 0, len(families))
	for _, family := range []hostresources.AddressFamily{hostresources.AddressFamilyIPv4, hostresources.AddressFamilyIPv6} {
		bind, ok := families[family]
		if !ok || port == 0 {
			continue
		}
		intent := hostresources.EndpointIntentPublic
		if address, err := netip.ParseAddr(bind); err == nil && address.IsLoopback() {
			intent = hostresources.EndpointIntentLocal
		}
		result = append(result, hostresources.ManagementEndpointV1{Schema: hostresources.ManagementEndpointSchemaV1,
			ID: endpointID(family, port), Network: hostresources.NetworkTCP, Family: family, Bind: bind, Port: port,
			ServiceKind: hostresources.ManagementSSH, Exposure: intent, Owner: "system", Purpose: "ssh_administrative_access",
			RecoveryPolicy: "fresh_independent_path_required", Source: "privileged_broker", ConfiguredIntent: true,
			Wildcard: bind == "0.0.0.0" || bind == "::", ConfidenceBP: 10000, ObservedAt: now.Unix(), ExpiresAt: now.Add(domain.MaxPostureLifetime).Unix(),
			ConfigurationRevision: configuration})
	}
	return result
}

func endpointID(family hostresources.AddressFamily, port uint16) string {
	return fmt.Sprintf("management:ssh:configured:%s:%d", family, port)
}

func endpointExists(posture domain.SSHPostureV1, id string) bool {
	for _, endpoint := range posture.Endpoints {
		if endpoint.ID == id {
			return true
		}
	}
	return false
}

func endpointMatches(id string, family hostresources.AddressFamily, port uint16) bool {
	return id == endpointID(family, port)
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

func ensureTicketRoot() error {
	info, err := os.Lstat(TicketRoot)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(TicketRoot, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(TicketRoot)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return errors.New("SSH proof root is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return errors.New("SSH proof root is not root-owned")
	}
	return nil
}

func ticketPath(operationID string) string {
	sum := sha256.Sum256([]byte(operationID))
	return filepath.Join(TicketRoot, hex.EncodeToString(sum[:])+".json")
}

func writeTicket(ticket proofTicketV1) error {
	if err := ensureTicketRoot(); err != nil || !safeToken(ticket.OperationID, 128) {
		return errors.New("SSH proof ticket identity is invalid")
	}
	data, err := json.Marshal(ticket)
	if err != nil || len(data) > 16<<10 {
		return errors.New("SSH proof ticket is too large")
	}
	path := ticketPath(ticket.OperationID)
	temporary, err := os.CreateTemp(TicketRoot, ".ticket-")
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
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if err := temporary.Chown(0, 0); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return syncDirectory(TicketRoot)
}

func readTicket(operationID string) (proofTicketV1, error) {
	if err := ensureTicketRoot(); err != nil {
		return proofTicketV1{}, err
	}
	data, _, _, err := secureRootFile(ticketPath(operationID), 16<<10)
	if err != nil {
		return proofTicketV1{}, err
	}
	var ticket proofTicketV1
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ticket); err != nil || ticket.Schema != 1 || ticket.OperationID != operationID {
		return proofTicketV1{}, errors.New("SSH proof ticket is malformed")
	}
	return ticket, nil
}

func readTickets() ([]proofTicketV1, error) {
	if err := ensureTicketRoot(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(TicketRoot)
	if err != nil || len(entries) > 128 {
		return nil, errors.New("SSH proof ticket cardinality is invalid")
	}
	result := make([]proofTicketV1, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || len(entry.Name()) != 69 {
			continue
		}
		path := filepath.Join(TicketRoot, entry.Name())
		data, _, _, err := secureRootFile(path, 16<<10)
		if err != nil {
			return nil, err
		}
		var ticket proofTicketV1
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&ticket); err != nil || ticket.Schema != 1 {
			return nil, errors.New("SSH proof ticket is malformed")
		}
		result = append(result, ticket)
	}
	return result, nil
}

type sessionProof struct {
	principalID         string
	authenticationClass string
	sourcePrefix        string
	family              hostresources.AddressFamily
	localPort           uint16
	authenticatedAt     int64
}

func (h *Host) proofSession(ctx context.Context, peer broker.PeerIdentity) (sessionProof, error) {
	account, err := user.LookupId(strconv.FormatUint(uint64(peer.UID), 10))
	if err != nil || !safeToken(account.Username, 64) {
		return sessionProof{}, errors.New("SSH peer account is unavailable")
	}
	local, remote, err := sshSessionSocket(peer.PID)
	if err != nil {
		return sessionProof{}, err
	}
	principalSum := sha256.Sum256([]byte("ssh:" + account.Username))
	principal := "principal:" + hex.EncodeToString(principalSum[:])
	observedAt, err := h.acceptedPublicKeyAt(ctx, account.Username, remote)
	if err != nil {
		return sessionProof{}, err
	}
	family := hostresources.AddressFamilyIPv6
	bits := 128
	if local.Addr().Is4() {
		family, bits = hostresources.AddressFamilyIPv4, 32
	}
	return sessionProof{principalID: principal, authenticationClass: "publickey", sourcePrefix: netip.PrefixFrom(remote.Addr(), bits).String(),
		family: family, localPort: local.Port(), authenticatedAt: observedAt}, nil
}

func (h *Host) acceptedPublicKeyAt(ctx context.Context, username string, remote netip.AddrPort) (int64, error) {
	output, err := h.run(ctx, h.journalctl, "--no-pager", "--output=json", "--unit=ssh.service", "--unit=sshd.service", "--since=-11min")
	if err != nil {
		return 0, err
	}
	latest := int64(0)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64<<10), maxCommandOutput)
	for scanner.Scan() {
		var row struct {
			Message    string `json:"MESSAGE"`
			Identifier string `json:"SYSLOG_IDENTIFIER"`
			Executable string `json:"_EXE"`
			Realtime   string `json:"__REALTIME_TIMESTAMP"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil || row.Identifier != "sshd" || row.Executable != h.sshd {
			continue
		}
		match := acceptedLogin.FindStringSubmatch(row.Message)
		if match == nil || match[2] != username || match[3] != remote.Addr().String() {
			continue
		}
		port, _ := strconv.ParseUint(match[4], 10, 16)
		micros, _ := strconv.ParseInt(row.Realtime, 10, 64)
		if uint16(port) == remote.Port() && micros/1_000_000 > latest {
			latest = micros / 1_000_000
		}
	}
	if latest == 0 {
		return 0, errors.New("fresh accepted public-key event is absent")
	}
	return latest, scanner.Err()
}

func sshSessionSocket(pid int) (netip.AddrPort, netip.AddrPort, error) {
	current := pid
	for depth := 0; depth < 12 && current > 1; depth++ {
		executable, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(current), "exe"))
		if filepath.Base(strings.TrimSuffix(executable, " (deleted)")) == "sshd" {
			if local, remote, err := processEstablishedSocket(current); err == nil {
				return local, remote, nil
			}
		}
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(current), "status"))
		if err != nil {
			break
		}
		parent := 0
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PPid:") {
				parent, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")))
				break
			}
		}
		if parent <= 1 || parent == current {
			break
		}
		current = parent
	}
	return netip.AddrPort{}, netip.AddrPort{}, errors.New("SSH session socket is unavailable")
}

func processEstablishedSocket(pid int) (netip.AddrPort, netip.AddrPort, error) {
	inodes := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	if err != nil || len(entries) > 4096 {
		return netip.AddrPort{}, netip.AddrPort{}, errors.New("SSH session descriptors are unavailable")
	}
	for _, entry := range entries {
		target, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", entry.Name()))
		if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
			inodes[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] = true
		}
	}
	for _, table := range []struct {
		name string
		v6   bool
	}{{"tcp", false}, {"tcp6", true}} {
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "net", table.name))
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
				return local, remote, nil
			}
		}
	}
	return netip.AddrPort{}, netip.AddrPort{}, errors.New("SSH established socket is unavailable")
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
