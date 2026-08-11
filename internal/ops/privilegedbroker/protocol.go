// Package privilegedbroker defines the only production panel-to-root
// authority boundary. The protocol contains typed semantic verbs; it has no
// command, argv, environment, arbitrary path, PID, or service-name escape
// hatch.
package privilegedbroker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	ProtocolVersion    = 1
	CapabilityRevision = "broker-capabilities-1.2"
	MaxRequestBytes    = 1 << 20
	MaxResponseBytes   = 256 << 10
	MaxArtifactBytes   = 512 << 10
	MaxFrameBytes      = MaxRequestBytes
	MaxJSONDepth       = 64
	MaxJSONMembers     = 4096
	DefaultSocketPath  = "/run/solovey-ui/privileged-broker.sock"
	ProofSocketPath    = "/run/solovey-ui/privileged-proof.sock"
	DefaultManifest    = "/etc/solovey-ui/broker-clients.json"
	DefaultJournalRoot = "/var/lib/solovey-ui-broker"
)

type Role string

const (
	RolePanel    Role = "panel"
	RoleSSHProof Role = "ssh-proof"
)

type Verb string

const (
	VerbCapabilities Verb = "broker.capabilities.observe"

	VerbSSHObserve  Verb = "ssh.posture.observe"
	VerbSSHStage    Verb = "ssh.dropin.stage"
	VerbSSHValidate Verb = "ssh.dropin.validate"
	VerbSSHReload   Verb = "ssh.service.reload"
	VerbSSHArm      Verb = "ssh.reconnect.arm"
	VerbSSHRestore  Verb = "ssh.dropin.restore"
	VerbSSHInspect  Verb = "ssh.dropin.inspect"
	VerbSSHVerify   Verb = "ssh.reconnect.verify"
	VerbSSHProof    Verb = "ssh.reconnect.proof"

	VerbDeploymentObserve  Verb = "deployment.posture.observe"
	VerbDeploymentDoctor   Verb = "deployment.doctor.observe"
	VerbDeploymentPrepare  Verb = "deployment.migration.prepare"
	VerbDeploymentApply    Verb = "deployment.migration.apply"
	VerbDeploymentVerify   Verb = "deployment.migration.verify"
	VerbDeploymentRollback Verb = "deployment.migration.rollback"

	VerbUpdateObserve  Verb = "update.release.observe"
	VerbUpdateStage    Verb = "update.artifact.stage"
	VerbUpdatePrepare  Verb = "update.release.prepare"
	VerbUpdateActivate Verb = "update.release.activate"
	VerbUpdateVerify   Verb = "update.release.verify"
	VerbUpdateRollback Verb = "update.release.rollback"
)

type Fence struct {
	Resource string `json:"resource"`
	Sequence uint64 `json:"sequence"`
	Token    string `json:"token"`
}

type Revisions struct {
	Provider      string `json:"provider,omitempty"`
	Binary        string `json:"binary,omitempty"`
	Service       string `json:"service,omitempty"`
	Configuration string `json:"configuration,omitempty"`
}

type Request struct {
	ProtocolVersion    int             `json:"protocolVersion"`
	CapabilityRevision string          `json:"capabilityRevision"`
	BootID             string          `json:"bootId"`
	Role               Role            `json:"role"`
	Verb               Verb            `json:"verb"`
	RequestID          string          `json:"requestId"`
	OperationID        string          `json:"operationId"`
	IdempotencyKey     string          `json:"idempotencyKey,omitempty"`
	Fence              Fence           `json:"fence"`
	Expected           Revisions       `json:"expected"`
	DeadlineAt         int64           `json:"deadlineAt"`
	Purpose            string          `json:"purpose,omitempty"`
	RecoveryRef        string          `json:"recoveryRef,omitempty"`
	PayloadDigest      string          `json:"payloadDigest"`
	Payload            json.RawMessage `json:"payload"`
}

type ErrorCode string

const (
	CodeInvalidRequest   ErrorCode = "invalid_request"
	CodeUnauthorized     ErrorCode = "unauthorized_peer"
	CodeUnsupported      ErrorCode = "unsupported_verb"
	CodeCapability       ErrorCode = "capability_unavailable"
	CodeDeadline         ErrorCode = "deadline_exceeded"
	CodeIdempotency      ErrorCode = "idempotency_conflict"
	CodeFence            ErrorCode = "stale_fence"
	CodeRevision         ErrorCode = "revision_mismatch"
	CodeRecoveryRequired ErrorCode = "recovery_required"
	CodeValidation       ErrorCode = "validation_failed"
	CodeExecution        ErrorCode = "execution_failed"
	CodeInternal         ErrorCode = "internal_error"
)

type Receipt struct {
	Sequence         uint64 `json:"sequence"`
	RequestID        string `json:"requestId"`
	OperationID      string `json:"operationId"`
	IdempotencyKey   string `json:"idempotencyKey"`
	Verb             Verb   `json:"verb"`
	FenceResource    string `json:"fenceResource"`
	FenceSequence    uint64 `json:"fenceSequence"`
	FenceTokenDigest string `json:"fenceTokenDigest"`
	PayloadDigest    string `json:"payloadDigest"`
	ResponseDigest   string `json:"responseDigest"`
	PeerRevision     string `json:"peerRevision"`
	BrokerBootID     string `json:"brokerBootId"`
	PreviousDigest   string `json:"previousDigest,omitempty"`
	Outcome          string `json:"outcome"`
	StartedAt        int64  `json:"startedAt"`
	CompletedAt      int64  `json:"completedAt"`
	ReceiptDigest    string `json:"receiptDigest"`
}

type Response struct {
	ProtocolVersion    int             `json:"protocolVersion"`
	CapabilityRevision string          `json:"capabilityRevision"`
	RequestID          string          `json:"requestId"`
	OperationID        string          `json:"operationId"`
	Verb               Verb            `json:"verb"`
	OK                 bool            `json:"ok"`
	Code               ErrorCode       `json:"code,omitempty"`
	Message            string          `json:"message,omitempty"`
	Replay             bool            `json:"replay,omitempty"`
	Receipt            *Receipt        `json:"receipt,omitempty"`
	PayloadDigest      string          `json:"payloadDigest,omitempty"`
	Payload            json.RawMessage `json:"payload,omitempty"`
}

type PeerIdentity struct {
	PID              int      `json:"pid"`
	UID              uint32   `json:"uid"`
	GID              uint32   `json:"gid"`
	Groups           []uint32 `json:"groups,omitempty"`
	Executable       string   `json:"executable"`
	ExecutableDigest string   `json:"executableDigest"`
	Device           uint64   `json:"device"`
	Inode            uint64   `json:"inode"`
	StartTime        string   `json:"startTime"`
	CgroupUnit       string   `json:"cgroupUnit"`
	BootID           string   `json:"bootId"`
	ManifestRevision string   `json:"manifestRevision"`
	Revision         string   `json:"revision"`
}

type Handler func(context.Context, Request, PeerIdentity) (any, error)

type Definition struct {
	Role     Role
	Mutation bool
	Handler  Handler
}

type Registry struct{ definitions map[Verb]Definition }

func NewRegistry() *Registry { return &Registry{definitions: make(map[Verb]Definition)} }

func (r *Registry) Register(verb Verb, definition Definition) error {
	if r == nil || !validVerb(string(verb)) || definition.Handler == nil ||
		definition.Role != RolePanel && definition.Role != RoleSSHProof {
		return errors.New("invalid broker handler definition")
	}
	if _, exists := r.definitions[verb]; exists {
		return fmt.Errorf("broker verb %q is already registered", verb)
	}
	r.definitions[verb] = definition
	return nil
}

func (r *Registry) definition(verb Verb) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	value, ok := r.definitions[verb]
	return value, ok
}

func (r *Registry) Verbs(role Role) []Verb {
	result := make([]Verb, 0, len(r.definitions))
	for verb, definition := range r.definitions {
		if definition.Role == role {
			result = append(result, verb)
		}
	}
	return result
}

type PublicError struct {
	Code ErrorCode
	Safe string
}

func (e *PublicError) Error() string {
	if e == nil || e.Safe == "" {
		return "broker operation failed"
	}
	return e.Safe
}

func Failure(code ErrorCode, safe string) error { return &PublicError{Code: code, Safe: safe} }

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@+-]{0,127}$`)
	verbPattern       = regexp.MustCompile(`^[a-z][a-z0-9_.-]{2,95}$`)
	digestPattern     = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func (r Request) Validate(now time.Time, definition Definition) error {
	if r.ProtocolVersion != ProtocolVersion || r.CapabilityRevision != CapabilityRevision {
		return Failure(CodeCapability, "broker protocol revision is unsupported")
	}
	if r.Role != definition.Role || !validVerb(string(r.Verb)) || !safeIdentifier(r.RequestID) || !safeIdentifier(r.OperationID) || r.BootID == "" || !safeIdentifier("boot-"+r.BootID) {
		return Failure(CodeInvalidRequest, "broker request identity is invalid")
	}
	if r.DeadlineAt <= now.UnixMilli() || r.DeadlineAt > now.Add(2*time.Minute).UnixMilli() {
		return Failure(CodeDeadline, "broker request deadline is invalid")
	}
	if len(r.Payload) == 0 || len(r.Payload) > MaxArtifactBytes*2 || !digestPattern.MatchString(r.PayloadDigest) || Digest(r.Payload) != r.PayloadDigest {
		return Failure(CodeInvalidRequest, "broker payload identity is invalid")
	}
	if len(r.Purpose) > 128 || len(r.RecoveryRef) > 256 {
		return Failure(CodeInvalidRequest, "broker request metadata is too large")
	}
	if definition.Mutation {
		if !safeIdentifier(r.IdempotencyKey) || !safeIdentifier(r.Fence.Resource) || r.Fence.Sequence == 0 || !digestPattern.MatchString(r.Fence.Token) {
			return Failure(CodeInvalidRequest, "broker mutation authority is invalid")
		}
	} else if r.IdempotencyKey != "" || r.Fence.Resource != "" || r.Fence.Sequence != 0 || r.Fence.Token != "" {
		return Failure(CodeInvalidRequest, "read-only broker verb contains mutation authority")
	}
	for _, revision := range []string{r.Expected.Provider, r.Expected.Binary, r.Expected.Service, r.Expected.Configuration} {
		if revision != "" && !digestPattern.MatchString(revision) {
			return Failure(CodeInvalidRequest, "expected revision is malformed")
		}
	}
	return nil
}

func safeIdentifier(value string) bool { return identifierPattern.MatchString(value) }
func validVerb(value string) bool      { return verbPattern.MatchString(value) }

func Digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func MarshalPayload(value any) (json.RawMessage, string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 || len(data) > MaxArtifactBytes*2 {
		return nil, "", errors.New("broker payload exceeds its bounded contract")
	}
	return data, Digest(data), nil
}

func DecodePayload(raw json.RawMessage, target any) error {
	if len(raw) == 0 || len(raw) > MaxArtifactBytes*2 {
		return errors.New("broker payload size is invalid")
	}
	return decodeStrict(raw, target)
}

func WriteFrame(writer io.Writer, value any, limit int) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > limit {
		return errors.New("broker frame exceeds its bounded contract")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func ReadFrame(reader io.Reader, target any, limit int) error {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return err
	}
	length := int(binary.BigEndian.Uint32(header[:]))
	if length <= 0 || length > limit {
		return errors.New("broker frame length is invalid")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return err
	}
	return decodeStrict(data, target)
}

func decodeStrict(data []byte, target any) error {
	if err := validateJSON(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("multiple JSON values are forbidden")
	}
	return nil
}

func validateJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	count := 0
	if err := walkJSON(decoder, 0, &count); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("multiple JSON values are forbidden")
	}
	return nil
}

func walkJSON(decoder *json.Decoder, depth int, count *int) error {
	if depth > MaxJSONDepth {
		return errors.New("JSON nesting is too deep")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	*count++
	if *count > MaxJSONMembers {
		return errors.New("JSON member cardinality is too large")
	}
	if text, ok := token.(string); ok && len(text) > MaxArtifactBytes*2 {
		return errors.New("JSON string is too large")
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			key, ok := keyToken.(string)
			if !ok || len(key) > 128 {
				return errors.New("JSON object key is invalid")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			*count++
			if *count > MaxJSONMembers {
				return errors.New("JSON member cardinality is too large")
			}
			if err := walkJSON(decoder, depth+1, count); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("JSON object is not terminated")
		}
	case '[':
		for decoder.More() {
			if err := walkJSON(decoder, depth+1, count); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("JSON array is not terminated")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

func publicFailure(err error) (ErrorCode, string) {
	var public *PublicError
	if errors.As(err, &public) && public.Code != "" {
		return public.Code, strings.TrimSpace(public.Safe)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return CodeDeadline, "broker operation deadline expired"
	}
	return CodeInternal, "broker operation failed"
}
