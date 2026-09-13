package privilegedbroker

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"sync"
	"time"
)

type Attestor interface {
	Attest(context.Context, *net.UnixConn, Role) (PeerIdentity, error)
	Recheck(context.Context, PeerIdentity, Role) error
}

type requestWriterAttestor interface {
	VerifyWriter(context.Context, PeerIdentity, WriterCredentials) error
}

type peerAttestationCloser interface {
	ClosePeer(PeerIdentity)
}

type Server struct {
	Registry          *Registry
	Journal           Journal
	Attestor          Attestor
	Audit             func(AuditEvent)
	Diagnostic        func(DiagnosticEvent)
	Now               func() time.Time
	BootID            string
	mutation          sync.Mutex
	limit             chan struct{}
	auditMu           sync.Mutex
	denials           map[string]uint64
	connMu            sync.Mutex
	conns             map[*net.UnixConn]struct{}
	connWG            sync.WaitGroup
	authorityMu       sync.Mutex
	authorityRevision string
	retiring          bool
	authorityActive   uint64
	authoritySequence uint64
	authorityCancels  map[uint64]context.CancelFunc
	authorityIdle     chan struct{}
}

// DiagnosticEvent is the closed broker transport for internal evidence sinks.
// The always-on recent ring consumes only the owner-classified fields below;
// an explicitly armed capture may additionally consume bounded peer facts. It
// contains no request payload, environment, command line or host-command
// output.
type DiagnosticEvent struct {
	Timestamp             time.Time
	Operation             Verb
	PeerRole              Role
	PeerAttestation       PeerAttestationClass
	ResultClass           string
	Phase                 AuditPhase
	Peer                  PeerIdentity
	Writer                WriterCredentials
	HandlerOwner          string
	HandlerReason         string
	HandlerStage          string
	HandlerErrno          string
	ProofMethod           string
	DescriptorCount       int
	SocketDescriptorCount int
	DuplicateAttempts     int
	DuplicatedSockets     int
	RetryCount            int
}

type handlerDiagnosticError interface {
	BrokerDiagnostic() (owner, reason, stage, errno, proofMethod string)
}

type handlerDiagnosticCounterError interface {
	BrokerDiagnosticCounters() (descriptorCount, socketDescriptorCount, duplicateAttempts, duplicatedSockets, retryCount int)
}

type handlerDiagnostic struct {
	owner, reason, stage, errno, proofMethod                  string
	descriptorCount, socketDescriptorCount, duplicateAttempts int
	duplicatedSockets, retryCount                             int
}

// AuditEvent deliberately contains only bounded broker-domain facts. It must
// never grow to include request payloads, paths, secrets, environment values,
// command output, or peer command lines.
type AuditEvent struct {
	Timestamp          int64      `json:"timestamp"`
	Verb               Verb       `json:"verb,omitempty"`
	OwnerDomain        string     `json:"ownerDomain"`
	OperationReference string     `json:"operationReference,omitempty"`
	PeerRole           Role       `json:"peerRole"`
	PeerRevision       string     `json:"peerRevision,omitempty"`
	PeerAttestation    string     `json:"peerAttestation,omitempty"`
	ResultClass        string     `json:"resultClass"`
	DurationClass      string     `json:"durationClass"`
	RevisionTransition string     `json:"revisionTransition"`
	RecoveryClass      string     `json:"recoveryClass"`
	AggregateCount     uint64     `json:"aggregateCount,omitempty"`
	Phase              AuditPhase `json:"phase"`
}

type AuditPhase string

const (
	AuditPhaseInitialAttestation AuditPhase = "initial_attestation"
	AuditPhaseRequestReceive     AuditPhase = "request_receive"
	AuditPhaseFinalRecheck       AuditPhase = "final_recheck"
	AuditPhaseDispatchGate       AuditPhase = "dispatch_gate"
)

type CapabilitiesV1 struct {
	ProtocolVersion    int       `json:"protocolVersion"`
	CapabilityRevision string    `json:"capabilityRevision"`
	Role               Role      `json:"role"`
	Verbs              []Verb    `json:"verbs"`
	Unresolved         []Receipt `json:"unresolved,omitempty"`
	Revision           string    `json:"revision"`
}

func NewServer(registry *Registry, journal Journal, attestor Attestor, bootID string) (*Server, error) {
	if registry == nil || journal == nil || attestor == nil || bootID == "" {
		return nil, errors.New("broker server dependencies are required")
	}
	server := &Server{Registry: registry, Journal: journal, Attestor: attestor, Now: time.Now,
		BootID: bootID, limit: make(chan struct{}, 32), denials: make(map[string]uint64, 16), conns: make(map[*net.UnixConn]struct{}),
		authorityCancels: make(map[uint64]context.CancelFunc), authorityIdle: make(chan struct{})}
	close(server.authorityIdle)
	if revision, ok := attestorRevision(attestor); ok {
		server.authorityRevision = revision
	} else {
		server.authorityRevision = Digest([]byte("initial-broker-authority"))
	}
	for _, role := range []Role{RolePanel, RoleSSHProof} {
		role := role
		if _, exists := registry.definition(VerbCapabilities); !exists && role == RolePanel {
			_ = registry.Register(VerbCapabilities, Definition{Role: role, Handler: server.capabilities})
		}
	}
	return server, nil
}

func (s *Server) Serve(ctx context.Context, listener *net.UnixListener, role Role) error {
	if s == nil || listener == nil || role != RolePanel && role != RoleSSHProof {
		return errors.New("broker listener is invalid")
	}
	if err := preparePeerListener(listener); err != nil {
		return errors.New("broker listener credential authority is unavailable")
	}
	for {
		if err := listener.SetDeadline(time.Now().Add(time.Second)); err != nil {
			return err
		}
		connection, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			return err
		}
		if err := preparePeerConnection(connection); err != nil {
			_ = connection.Close()
			continue
		}
		select {
		case s.limit <- struct{}{}:
			s.connMu.Lock()
			s.conns[connection] = struct{}{}
			s.connWG.Add(1)
			s.connMu.Unlock()
			go func() {
				defer func() {
					s.connMu.Lock()
					delete(s.conns, connection)
					s.connMu.Unlock()
					s.connWG.Done()
					<-s.limit
				}()
				s.serveConnection(ctx, connection, role)
			}()
		default:
			_ = connection.Close()
		}
	}
}

// ShutdownConnections interrupts active framed requests after listeners have
// stopped accepting. WaitConnections can then prove that no handler or audit
// callback survives broker shutdown.
func (s *Server) ShutdownConnections() {
	if s == nil {
		return
	}
	s.connMu.Lock()
	for connection := range s.conns {
		_ = connection.Close()
	}
	s.connMu.Unlock()
}

func (s *Server) WaitConnections() {
	if s != nil {
		s.connWG.Wait()
	}
}

func (s *Server) serveConnection(ctx context.Context, connection *net.UnixConn, role Role) {
	defer connection.Close()
	started := time.Now()
	lease, err := s.beginAuthority(ctx)
	if err != nil {
		event := AuditEvent{OwnerDomain: "broker", PeerRole: role, PeerAttestation: string(peerAttestationClass(err)), ResultClass: "denied_peer", DurationClass: durationClass(time.Since(started)), RevisionTransition: "none", RecoveryClass: "none", Phase: AuditPhaseInitialAttestation}
		s.emitDiagnostic(event, PeerIdentity{}, WriterCredentials{})
		s.emitAudit(event, true)
		_ = WriteFrame(connection, failureResponse(Request{}, CodeUnauthorized, "broker authority generation is unavailable"), MaxResponseBytes)
		return
	}
	defer lease.Release()
	ctx = lease.Context
	attestor := lease.Attestor
	_ = connection.SetDeadline(time.Now().Add(2 * time.Minute))
	peer, err := attestor.Attest(ctx, connection, role)
	if err == nil {
		if closer, ok := attestor.(peerAttestationCloser); ok {
			defer closer.ClosePeer(peer)
		}
	}
	if err != nil || peer.BootID != s.BootID || lease.Revisioned && peer.ManifestRevision != lease.Revision {
		class := peerAttestationClass(err)
		if err == nil && peer.BootID != s.BootID {
			class = PeerAttestationBootMismatch
		} else if err == nil && lease.Revisioned && peer.ManifestRevision != lease.Revision {
			class = PeerAttestationGenerationMismatch
		}
		event := AuditEvent{OwnerDomain: "broker", PeerRole: role, PeerAttestation: string(class), ResultClass: "denied_peer", DurationClass: durationClass(time.Since(started)), RevisionTransition: "none", RecoveryClass: "none", Phase: AuditPhaseInitialAttestation}
		s.emitDiagnostic(event, peer, WriterCredentials{})
		s.emitAudit(event, true)
		_ = WriteFrame(connection, failureResponse(Request{}, CodeUnauthorized, "broker peer is not authorized"), MaxResponseBytes)
		return
	}
	var request Request
	writer, err := readPeerRequest(connection, &request, MaxRequestBytes)
	if err != nil {
		event := s.auditEvent(request, peer, failureResponse(request, CodeInvalidRequest, "broker request is malformed"), time.Since(started))
		event.Phase = AuditPhaseRequestReceive
		s.emitDiagnostic(event, peer, writer)
		s.emitAudit(event, true)
		_ = WriteFrame(connection, failureResponse(request, CodeInvalidRequest, "broker request is malformed"), MaxResponseBytes)
		return
	}
	writerAttestor, writerAware := attestor.(requestWriterAttestor)
	if writerAware {
		if err := writerAttestor.VerifyWriter(ctx, peer, writer); err != nil {
			event := s.auditEvent(request, peer, failureResponse(request, CodeUnauthorized, "broker request writer is not authorized"), time.Since(started))
			event.PeerAttestation = string(peerAttestationClass(err))
			event.Phase = AuditPhaseFinalRecheck
			s.emitDiagnostic(event, peer, writer)
			s.emitAudit(event, true)
			_ = WriteFrame(connection, failureResponse(request, CodeUnauthorized, "broker request writer is not authorized"), MaxResponseBytes)
			return
		}
	}
	if request.Role != role {
		event := s.auditEvent(request, peer, failureResponse(request, CodeUnauthorized, "broker socket role does not match request"), time.Since(started))
		event.Phase = AuditPhaseRequestReceive
		s.emitDiagnostic(event, peer, writer)
		s.emitAudit(event, true)
		_ = WriteFrame(connection, failureResponse(request, CodeUnauthorized, "broker socket role does not match request"), MaxResponseBytes)
		return
	}
	gate := func() error {
		if ctx.Err() != nil || !s.authorityCurrent(lease.Revision) {
			return attestationFailure(PeerAttestationGenerationMismatch, errors.New("broker authority generation changed before dispatch"))
		}
		if writerAware {
			if err := writerAttestor.VerifyWriter(ctx, peer, writer); err != nil {
				return err
			}
		}
		if err := attestor.Recheck(ctx, peer, role); err != nil {
			return err
		}
		if lease.Revisioned && peer.ManifestRevision != lease.Revision {
			return attestationFailure(PeerAttestationGenerationMismatch, errors.New("broker peer manifest generation changed"))
		}
		return nil
	}
	response := s.handle(ctx, request, peer, gate)
	_ = WriteFrame(connection, response, MaxResponseBytes)
}

func (s *Server) Handle(ctx context.Context, request Request, peer PeerIdentity) Response {
	return s.handle(ctx, request, peer, nil)
}

func (s *Server) handle(ctx context.Context, request Request, peer PeerIdentity, gate func() error) Response {
	if ctx == nil {
		ctx = context.Background()
	}
	started := time.Now()
	var response Response
	var gateClass PeerAttestationClass
	var handlerFailure handlerDiagnostic
	defer func() {
		event := s.auditEvent(request, peer, response, time.Since(started))
		event.PeerAttestation = string(gateClass)
		if !response.OK {
			s.emitDiagnostic(event, peer, WriterCredentials{}, handlerFailure)
		}
		s.emitAudit(event, !response.OK)
	}()
	if peer.CapabilitiesOnly && request.Verb != VerbCapabilities {
		response = failureResponse(request, CodeUnauthorized, "broker client capability is restricted")
		return response
	}
	definition, exists := s.Registry.definition(request.Verb)
	if !exists {
		response = failureResponse(request, CodeUnsupported, "broker verb is not registered")
		return response
	}
	if request.BootID != s.BootID {
		response = failureResponse(request, CodeCapability, "broker boot identity changed")
		return response
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	if err := request.Validate(now, definition); err != nil {
		code, message := publicFailure(err)
		response = failureResponse(request, code, message)
		return response
	}
	deadline := time.UnixMilli(request.DeadlineAt)
	operationContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	requestDigest := Digest(append(canonicalRequestAuthority(request), request.Payload...))
	var receipt *Receipt
	checkGate := func() bool {
		if gate == nil {
			return true
		}
		if err := gate(); err != nil {
			gateClass = peerAttestationClass(err)
			response = failureResponse(request, CodeUnauthorized, "broker peer identity changed")
			return false
		}
		return true
	}
	if definition.Mutation {
		s.mutation.Lock()
		defer s.mutation.Unlock()
		if !checkGate() {
			return response
		}
		replay, active, err := s.Journal.Begin(request, peer, requestDigest, now)
		if err != nil {
			handlerFailure = handlerDiagnosticFromError(err)
			code, message := publicFailure(err)
			response = failureResponse(request, code, message)
			return response
		}
		if replay != nil {
			response = *replay
			return response
		}
		receipt = active
	}
	if checkGate() {
		result, err := invokeHandler(operationContext, definition, request, peer)
		response = successResponse(request)
		if err != nil {
			handlerFailure = handlerDiagnosticFromError(err)
			code, message := publicFailure(err)
			response = failureResponse(request, code, message)
		} else if result != nil {
			payload, digest, marshalErr := MarshalPayload(result)
			if marshalErr != nil || len(payload)+4096 > MaxResponseBytes {
				response = failureResponse(request, CodeInternal, "broker response exceeds its bounded contract")
			} else {
				response.Payload, response.PayloadDigest = payload, digest
			}
		}
	}
	if definition.Mutation {
		policy := CompletionPolicy{RetainUntilRelease: definition.RetainResultUntilRelease,
			ReleasesVerb: definition.ReleasesRetainedResultVerb}
		committed, commitErr := s.Journal.Commit(request, receipt, response, policy, time.Now().UTC())
		if commitErr != nil {
			response = failureResponse(request, CodeRecoveryRequired, "broker receipt persistence requires recovery")
			return response
		}
		response = committed
	}
	return response
}

func invokeHandler(ctx context.Context, definition Definition, request Request, peer PeerIdentity) (result any, err error) {
	defer func() {
		if recover() != nil {
			code := CodeInternal
			message := "broker handler failed"
			if definition.Mutation {
				code = CodeRecoveryRequired
				message = "broker mutation outcome requires recovery"
			}
			result = nil
			err = Failure(code, message)
		}
	}()
	return definition.Handler(ctx, request, peer)
}

func (s *Server) auditEvent(request Request, peer PeerIdentity, response Response, elapsed time.Duration) AuditEvent {
	result := "success"
	recovery := "none"
	if response.Replay {
		result = "replay"
	} else if !response.OK {
		result = "denied_" + auditResultClass(response.Code)
	}
	if response.Code == CodeRecoveryRequired {
		recovery = "manual_recovery_required"
	}
	revision := "none"
	if request.Expected.Provider != "" || request.Expected.Binary != "" || request.Expected.Service != "" || request.Expected.Configuration != "" {
		revision = "expected_revision_present"
	}
	return AuditEvent{
		Verb: sanitizeAuditVerb(request.Verb), OwnerDomain: ownerDomain(request.Verb), OperationReference: sanitizeAuditOperation(request.OperationID),
		PeerRole: request.Role, PeerRevision: sanitizeAuditDigest(peer.Revision), ResultClass: result,
		DurationClass: durationClass(elapsed), RevisionTransition: revision, RecoveryClass: recovery,
		Phase: AuditPhaseDispatchGate,
	}
}

func auditResultClass(code ErrorCode) string {
	switch code {
	case CodeInvalidRequest, CodeUnauthorized, CodeUnsupported, CodeCapability,
		CodeDeadline, CodeIdempotency, CodeFence, CodeRevision,
		CodeRecoveryRequired, CodeValidation, CodeExecution, CodeInternal:
		return string(code)
	default:
		return string(CodeInternal)
	}
}

func (s *Server) emitAudit(event AuditEvent, denial bool) {
	if s == nil || s.Audit == nil {
		return
	}
	if event.PeerRole != RolePanel && event.PeerRole != RoleSSHProof {
		event.PeerRole = "unknown"
	}
	event.Verb = sanitizeAuditVerb(event.Verb)
	event.OperationReference = sanitizeAuditOperation(event.OperationReference)
	event.PeerRevision = sanitizeAuditDigest(event.PeerRevision)
	event.PeerAttestation = string(sanitizePeerAttestation(PeerAttestationClass(event.PeerAttestation)))
	event.Phase = sanitizeAuditPhase(event.Phase)
	event.ResultClass = sanitizeAuditResult(event.ResultClass)
	event.DurationClass = sanitizeAuditDuration(event.DurationClass)
	event.RevisionTransition = sanitizeRevisionTransition(event.RevisionTransition)
	event.RecoveryClass = sanitizeRecoveryClass(event.RecoveryClass)
	event.OwnerDomain = ownerDomain(event.Verb)
	event.Timestamp = time.Now().UTC().Unix()
	if s.Now != nil {
		event.Timestamp = s.Now().UTC().Unix()
	}
	if denial {
		// The key has fixed-cardinality enum fields only. Emitting the first and
		// power-of-two occurrences bounds denial-log amplification while retaining
		// an exponentially increasing count of persistent abuse or drift.
		key := string(event.PeerRole) + "\x00" + event.ResultClass + "\x00" + event.PeerAttestation
		s.auditMu.Lock()
		count := s.denials[key]
		if count < ^uint64(0) {
			count++
		}
		s.denials[key] = count
		s.auditMu.Unlock()
		if count != 1 && count&(count-1) != 0 {
			return
		}
		event.AggregateCount = count
	}
	s.Audit(event)
}

func (s *Server) emitDiagnostic(event AuditEvent, peer PeerIdentity, writer WriterCredentials, handlerFailures ...handlerDiagnostic) {
	if s == nil || s.Diagnostic == nil {
		return
	}
	role := event.PeerRole
	if role != RolePanel && role != RoleSSHProof {
		role = "unknown"
	}
	at := time.Now().UTC()
	if s.Now != nil {
		at = s.Now().UTC()
	}
	var handlerFailure handlerDiagnostic
	if len(handlerFailures) == 1 {
		handlerFailure = handlerFailures[0]
	}
	s.Diagnostic(DiagnosticEvent{Timestamp: at, Operation: sanitizeAuditVerb(event.Verb), PeerRole: role,
		PeerAttestation: sanitizePeerAttestation(PeerAttestationClass(event.PeerAttestation)),
		ResultClass:     sanitizeAuditResult(event.ResultClass), Phase: sanitizeAuditPhase(event.Phase),
		Peer: peer, Writer: writer,
		HandlerOwner: handlerFailure.owner, HandlerReason: handlerFailure.reason, HandlerStage: handlerFailure.stage,
		HandlerErrno: handlerFailure.errno, ProofMethod: handlerFailure.proofMethod,
		DescriptorCount: handlerFailure.descriptorCount, SocketDescriptorCount: handlerFailure.socketDescriptorCount,
		DuplicateAttempts: handlerFailure.duplicateAttempts, DuplicatedSockets: handlerFailure.duplicatedSockets,
		RetryCount: handlerFailure.retryCount})
}

func handlerDiagnosticFromError(err error) handlerDiagnostic {
	var typed handlerDiagnosticError
	if !errors.As(err, &typed) {
		return handlerDiagnostic{}
	}
	owner, reason, stage, errno, proofMethod := typed.BrokerDiagnostic()
	diagnostic := handlerDiagnostic{
		owner: sanitizeHandlerDiagnostic(owner), reason: sanitizeHandlerDiagnostic(reason), stage: sanitizeHandlerDiagnostic(stage),
		errno: sanitizeHandlerDiagnostic(errno), proofMethod: sanitizeHandlerDiagnostic(proofMethod),
	}
	var counters handlerDiagnosticCounterError
	if errors.As(err, &counters) {
		diagnostic.descriptorCount, diagnostic.socketDescriptorCount, diagnostic.duplicateAttempts,
			diagnostic.duplicatedSockets, diagnostic.retryCount = counters.BrokerDiagnosticCounters()
		if !validHandlerDiagnosticCounters(diagnostic) {
			diagnostic.descriptorCount, diagnostic.socketDescriptorCount, diagnostic.duplicateAttempts,
				diagnostic.duplicatedSockets, diagnostic.retryCount = 0, 0, 0, 0, 0
		}
	}
	return diagnostic
}

func validHandlerDiagnosticCounters(value handlerDiagnostic) bool {
	const maximum = 1 << 20
	return value.descriptorCount >= 0 && value.descriptorCount <= maximum &&
		value.socketDescriptorCount >= 0 && value.socketDescriptorCount <= maximum &&
		value.duplicateAttempts >= 0 && value.duplicateAttempts <= maximum &&
		value.duplicatedSockets >= 0 && value.duplicatedSockets <= maximum &&
		value.retryCount >= 0 && value.retryCount <= maximum
}

func sanitizeHandlerDiagnostic(value string) string {
	if value == "" || len(value) > 96 {
		return ""
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' ||
			character == '_' || character == '-' || character == '.' || character == '/' {
			continue
		}
		return ""
	}
	return value
}

func ownerDomain(verb Verb) string {
	switch sanitizeAuditVerb(verb) {
	case VerbSSHObserve, VerbSSHPrepare, VerbSSHStage, VerbSSHRecoverStage, VerbSSHReleaseStage, VerbSSHValidate, VerbSSHReload, VerbSSHArm, VerbSSHRestore, VerbSSHInspect, VerbSSHVerify, VerbSSHProof:
		return "ssh"
	case VerbDeploymentObserve, VerbDeploymentDoctor, VerbDeploymentPrepare, VerbDeploymentApply, VerbDeploymentVerify, VerbDeploymentRollback:
		return "deployment"
	case VerbUpdateObserve, VerbUpdateStage, VerbUpdateRelease, VerbUpdatePrepare, VerbUpdateActivate, VerbUpdateVerify, VerbUpdateRollback:
		return "update"
	}
	return "broker"
}

func durationClass(elapsed time.Duration) string {
	switch {
	case elapsed < 10*time.Millisecond:
		return "lt_10ms"
	case elapsed < 100*time.Millisecond:
		return "lt_100ms"
	case elapsed < time.Second:
		return "lt_1s"
	case elapsed < 10*time.Second:
		return "lt_10s"
	default:
		return "gte_10s"
	}
}

func (s *Server) capabilities(_ context.Context, request Request, peer PeerIdentity) (any, error) {
	verbs := s.Registry.Verbs(request.Role)
	if peer.CapabilitiesOnly {
		verbs = []Verb{VerbCapabilities}
	}
	sort.Slice(verbs, func(left, right int) bool { return verbs[left] < verbs[right] })
	result := CapabilitiesV1{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision,
		Role: request.Role, Verbs: verbs, Unresolved: s.Journal.Unresolved()}
	data, _ := json.Marshal(result)
	result.Revision = Digest(data)
	return result, nil
}

func successResponse(request Request) Response {
	return Response{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision,
		RequestID: request.RequestID, OperationID: request.OperationID, Verb: request.Verb, OK: true}
}

func failureResponse(request Request, code ErrorCode, message string) Response {
	if len(message) > 160 {
		message = "broker operation failed"
	}
	return Response{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision,
		RequestID: request.RequestID, OperationID: request.OperationID, Verb: request.Verb,
		Code: code, Message: message}
}
