package privilegedbroker

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

type Attestor interface {
	Attest(context.Context, *net.UnixConn, Role) (PeerIdentity, error)
	Recheck(context.Context, PeerIdentity, Role) error
}

type Server struct {
	Registry *Registry
	Journal  Journal
	Attestor Attestor
	Audit    func(AuditEvent)
	Now      func() time.Time
	BootID   string
	mutation sync.Mutex
	limit    chan struct{}
	auditMu  sync.Mutex
	denials  map[string]uint64
	connMu   sync.Mutex
	conns    map[*net.UnixConn]struct{}
	connWG   sync.WaitGroup
}

// AuditEvent deliberately contains only bounded broker-domain facts. It must
// never grow to include request payloads, paths, secrets, environment values,
// command output, or peer command lines.
type AuditEvent struct {
	Timestamp          int64  `json:"timestamp"`
	Verb               Verb   `json:"verb,omitempty"`
	OwnerDomain        string `json:"ownerDomain"`
	OperationReference string `json:"operationReference,omitempty"`
	PeerRole           Role   `json:"peerRole"`
	PeerRevision       string `json:"peerRevision,omitempty"`
	ResultClass        string `json:"resultClass"`
	DurationClass      string `json:"durationClass"`
	RevisionTransition string `json:"revisionTransition"`
	RecoveryClass      string `json:"recoveryClass"`
	AggregateCount     uint64 `json:"aggregateCount,omitempty"`
}

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
		BootID: bootID, limit: make(chan struct{}, 32), denials: make(map[string]uint64, 16), conns: make(map[*net.UnixConn]struct{})}
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
	_ = connection.SetDeadline(time.Now().Add(2 * time.Minute))
	peer, err := s.Attestor.Attest(ctx, connection, role)
	if err != nil || peer.BootID != s.BootID {
		s.emitAudit(AuditEvent{OwnerDomain: "broker", PeerRole: role, ResultClass: "denied_peer", DurationClass: durationClass(time.Since(started)), RevisionTransition: "none", RecoveryClass: "none"}, true)
		_ = WriteFrame(connection, failureResponse(Request{}, CodeUnauthorized, "broker peer is not authorized"), MaxResponseBytes)
		return
	}
	var request Request
	if err := ReadFrame(connection, &request, MaxRequestBytes); err != nil {
		s.emitAudit(s.auditEvent(request, peer, failureResponse(request, CodeInvalidRequest, "broker request is malformed"), time.Since(started)), true)
		_ = WriteFrame(connection, failureResponse(request, CodeInvalidRequest, "broker request is malformed"), MaxResponseBytes)
		return
	}
	if request.Role != role {
		s.emitAudit(s.auditEvent(request, peer, failureResponse(request, CodeUnauthorized, "broker socket role does not match request"), time.Since(started)), true)
		_ = WriteFrame(connection, failureResponse(request, CodeUnauthorized, "broker socket role does not match request"), MaxResponseBytes)
		return
	}
	if err := s.Attestor.Recheck(ctx, peer, role); err != nil {
		s.emitAudit(s.auditEvent(request, peer, failureResponse(request, CodeUnauthorized, "broker peer identity changed"), time.Since(started)), true)
		_ = WriteFrame(connection, failureResponse(request, CodeUnauthorized, "broker peer identity changed"), MaxResponseBytes)
		return
	}
	response := s.Handle(ctx, request, peer)
	_ = WriteFrame(connection, response, MaxResponseBytes)
}

func (s *Server) Handle(ctx context.Context, request Request, peer PeerIdentity) Response {
	if ctx == nil {
		ctx = context.Background()
	}
	started := time.Now()
	var response Response
	defer func() {
		s.emitAudit(s.auditEvent(request, peer, response, time.Since(started)), !response.OK)
	}()
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
	if definition.Mutation {
		s.mutation.Lock()
		defer s.mutation.Unlock()
		replay, active, err := s.Journal.Begin(request, peer, requestDigest, now)
		if err != nil {
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
	result, err := invokeHandler(operationContext, definition, request, peer)
	response = successResponse(request)
	if err != nil {
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
	if definition.Mutation {
		committed, commitErr := s.Journal.Commit(request, receipt, response, time.Now().UTC())
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
		Verb: request.Verb, OwnerDomain: ownerDomain(request.Verb), OperationReference: request.OperationID,
		PeerRole: request.Role, PeerRevision: peer.Revision, ResultClass: result,
		DurationClass: durationClass(elapsed), RevisionTransition: revision, RecoveryClass: recovery,
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
	event.Timestamp = time.Now().UTC().Unix()
	if s.Now != nil {
		event.Timestamp = s.Now().UTC().Unix()
	}
	if denial {
		// The key has fixed-cardinality enum fields only. Emitting the first and
		// power-of-two occurrences bounds denial-log amplification while retaining
		// an exponentially increasing count of persistent abuse or drift.
		key := string(event.PeerRole) + "\x00" + event.ResultClass
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

func ownerDomain(verb Verb) string {
	value := string(verb)
	if index := strings.IndexByte(value, '.'); index > 0 {
		return value[:index]
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

func (s *Server) capabilities(_ context.Context, request Request, _ PeerIdentity) (any, error) {
	verbs := s.Registry.Verbs(request.Role)
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
