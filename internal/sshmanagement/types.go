// Package sshmanagement contains the pure, host-agnostic SSH management
// recovery model. It has no filesystem, process, service-manager or network
// authority.
package sshmanagement

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	PostureSchemaV1              = "solovey-ui/ssh-posture/v1"
	PolicySchemaV1               = "solovey-ui/ssh-managed-policy/v1"
	PreservationSchemaV1         = "solovey-ui/management-preservation-plan/v1"
	CandidateSchemaV1            = "solovey-ui/ssh-management-candidate/v1"
	ChallengeSchemaV1            = "solovey-ui/ssh-reconnect-challenge/v1"
	ListenerAuthoritySchemaV1    = "solovey-ui/ssh-listener-authority/v1"
	MaxPostureLifetime           = 5 * time.Minute
	MaxRecoveryLifetime          = 15 * time.Minute
	MaxChallengeLifetime         = 10 * time.Minute
	MaxListenerAuthorityLifetime = 2 * time.Minute
)

type ReasonCode string

const (
	ReasonProviderUnavailable       ReasonCode = "provider_unavailable"
	ReasonPostureStale              ReasonCode = "ssh_posture_stale"
	ReasonPostureAmbiguous          ReasonCode = "ssh_posture_ambiguous"
	ReasonUnsupportedDirective      ReasonCode = "unsupported_directive"
	ReasonUnknownMatchContext       ReasonCode = "unknown_match_context"
	ReasonUnsafeOwner               ReasonCode = "unsafe_owner"
	ReasonUnsafeMode                ReasonCode = "unsafe_mode"
	ReasonSymlink                   ReasonCode = "symlink_not_allowed"
	ReasonBinaryMismatch            ReasonCode = "binary_mismatch"
	ReasonConfigurationMismatch     ReasonCode = "configuration_mismatch"
	ReasonRecoveryPathMissing       ReasonCode = "independent_recovery_path_missing"
	ReasonConsoleMissing            ReasonCode = "provider_console_missing"
	ReasonFreshPubkeyMissing        ReasonCode = "fresh_pubkey_reconnect_missing"
	ReasonManagementPathRemoved     ReasonCode = "last_management_path_removed"
	ReasonEndpointAmbiguous         ReasonCode = "management_endpoint_ambiguous"
	ReasonRevisionMismatch          ReasonCode = "revision_mismatch"
	ReasonExpiryTooNear             ReasonCode = "safety_evidence_expiry_too_near"
	ReasonAcknowledgementMissing    ReasonCode = "operator_acknowledgement_missing"
	ReasonWatchdogMissing           ReasonCode = "watchdog_unavailable"
	ReasonProductionMutationAbsent  ReasonCode = "production_mutation_provider_unavailable"
	ReasonReconnectProofInvalid     ReasonCode = "reconnect_proof_invalid"
	ReasonRollbackConflict          ReasonCode = "rollback_foreign_newer_state"
	ReasonRollbackVerification      ReasonCode = "rollback_verification_failed"
	ReasonRestoredStateUntrusted    ReasonCode = "restored_state_untrusted"
	ReasonCandidateAlreadyActive    ReasonCode = "candidate_already_active"
	ReasonIdempotencyConflict       ReasonCode = "idempotency_conflict"
	ReasonOperationStateConflict    ReasonCode = "operation_state_conflict"
	ReasonMalformedProviderEvidence ReasonCode = "malformed_provider_evidence"
	ReasonRecoveryIdentityUnknown   ReasonCode = "recovery_identity_unknown"
	ReasonRecoveryIdentityInvalid   ReasonCode = "recovery_identity_invalid"
	ReasonRecoveryEndpointAmbiguous ReasonCode = "recovery_endpoint_ambiguous"
	ReasonRecoveryBindingStale      ReasonCode = "recovery_identity_binding_stale"
	ReasonRecoveryEvidenceInvalid   ReasonCode = "recovery_evidence_invalid"
	ReasonRecoveryPersistFailed     ReasonCode = "recovery_persist_failed"
	ReasonRecoveryReauthRequired    ReasonCode = "recovery_reauthentication_required"
)

type Error struct {
	Code ReasonCode
	Op   string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Op == "" {
		return string(e.Code)
	}
	return e.Op + ": " + string(e.Code)
}

func NewError(op string, code ReasonCode) error { return &Error{Code: code, Op: op} }

func ErrorCode(err error) ReasonCode {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ReasonMalformedProviderEvidence
}

type Availability string

const (
	AvailabilityAvailable   Availability = "AVAILABLE"
	AvailabilityUnavailable Availability = "UNAVAILABLE"
	AvailabilityUnknown     Availability = "UNKNOWN"
)

type CapabilitySetV1 struct {
	ObservePosture Availability `json:"observePosture"`
	Prepare        Availability `json:"prepare"`
	Stage          Availability `json:"stage"`
	Validate       Availability `json:"validate"`
	Reload         Availability `json:"reload"`
	Reconnect      Availability `json:"reconnect"`
	Rollback       Availability `json:"rollback"`
	ReasonCodes    []ReasonCode `json:"reasonCodes,omitempty"`
	Revision       string       `json:"revision"`
}

type BinaryIdentityV1 struct {
	Implementation string `json:"implementation"`
	VersionClass   string `json:"versionClass"`
	// Digest is the raw executable-content SHA-256 supplied by the executable
	// object owner. It is intentionally the same domain as Process.ExeDigest.
	Digest   string `json:"digest"`
	Selected bool   `json:"selected"`
}

type ServiceIdentityV1 struct {
	Manager string `json:"manager"`
	UnitID  string `json:"unitId"`
	State   string `json:"state"`
	Digest  string `json:"digest"`
}

// SSHListenerAuthorityV1 is the root-observed proof that an exact SSH
// supervisor instance owns an exact accepting socket. Configured endpoints
// remain policy intent; consumers must not promote them to runtime ownership
// without one of these short-lived authorities.
type SSHListenerAuthorityV1 struct {
	Schema      string                             `json:"schema"`
	Revision    string                             `json:"revision"`
	EndpointIDs []string                           `json:"endpointIds"`
	InstanceID  string                             `json:"instanceId"`
	Socket      hostfacts.ListenerSocketIdentityV1 `json:"socket"`
	Process     hostfacts.ProcessFact              `json:"process"`
	Service     hostfacts.ServiceFact              `json:"service"`
	// BinaryRevision is the released field name for the selected executable's
	// raw content SHA-256. It must equal Binary.Digest and Process.ExeDigest.
	BinaryRevision        string `json:"binaryRevision"`
	ServiceRevision       string `json:"serviceRevision"`
	ConfigurationRevision string `json:"configurationRevision"`
	ObservedAt            int64  `json:"observedAt"`
	ExpiresAt             int64  `json:"expiresAt"`
}

func (a *SSHListenerAuthorityV1) Seal() {
	if a == nil {
		return
	}
	a.EndpointIDs = normalizedAuthorityEndpointIDs(a.EndpointIDs)
	copy := *a
	copy.Revision = ""
	copy.ObservedAt, copy.ExpiresAt = 0, 0
	a.Revision = Revision(copy)
}

func (a SSHListenerAuthorityV1) Valid(now time.Time) bool {
	current := now.UTC().Unix()
	if a.Schema != ListenerAuthoritySchemaV1 || !digest(a.Revision) || a.ObservedAt <= 0 || a.ObservedAt > current ||
		a.ExpiresAt <= a.ObservedAt || a.ExpiresAt > a.ObservedAt+int64(MaxListenerAuthorityLifetime/time.Second) || a.ExpiresAt <= now.UTC().Unix() ||
		!token(a.InstanceID, 128) || !digest(a.BinaryRevision) || !digest(a.ServiceRevision) || !digest(a.ConfigurationRevision) ||
		len(a.EndpointIDs) == 0 || len(a.EndpointIDs) > 32 || !validAuthoritySocket(a.Socket) ||
		!validAuthorityProcess(a.Process) || !validAuthorityService(a.Service, a.Process) || !authorityInstanceMatchesService(a) || a.Process.ExeDigest != a.BinaryRevision {
		return false
	}
	for index, id := range a.EndpointIDs {
		if !token(id, 256) || index > 0 && id == a.EndpointIDs[index-1] {
			return false
		}
	}
	copy := a
	copy.Seal()
	return copy.Revision == a.Revision && len(copy.EndpointIDs) == len(a.EndpointIDs)
}

func authorityInstanceMatchesService(value SSHListenerAuthorityV1) bool {
	if value.Service.ProcdInstance != "" {
		return value.Service.ProcdInstance == value.InstanceID
	}
	return value.Service.SystemdUnit == value.InstanceID
}

func validAuthoritySocket(value hostfacts.ListenerSocketIdentityV1) bool {
	return value.Network == hostfacts.NetworkTCP && hostfacts.ValidListenerSocketIdentity(value)
}

func validAuthorityProcess(value hostfacts.ProcessFact) bool {
	return value.PID != nil && value.ParentPID != nil && value.SessionID != nil && value.UID != nil && value.GID != nil &&
		*value.PID > 1 && *value.ParentPID >= 0 && *value.SessionID >= 0 && *value.UID >= 0 && *value.GID >= 0 &&
		numeric(value.StartTime) && digest(value.EvidenceRevision) && digest(value.ExeDigest) && value.ExeDevice != 0 && value.ExeInode != 0 &&
		canonicalAuthorityPath(value.Executable) && safeAuthorityProvider(value.ProviderRevision) &&
		(value.ControlGroup == "" || canonicalAuthorityPath(value.ControlGroup))
}

func validAuthorityService(value hostfacts.ServiceFact, process hostfacts.ProcessFact) bool {
	if value.MainPID == nil || process.PID == nil || *value.MainPID != *process.PID || value.ActiveState != "active" || value.SubState != "running" ||
		!digest(value.SupervisorRevision) || !digest(value.CgroupRevision) ||
		value.CgroupAvailability != "available" && value.CgroupAvailability != "unavailable" ||
		value.CgroupPolicy != "required" && value.CgroupPolicy != "optional" || value.ControlGroup != process.ControlGroup {
		return false
	}
	if value.CgroupAvailability == "available" && !canonicalAuthorityPath(value.ControlGroup) || value.CgroupAvailability == "unavailable" && value.ControlGroup != "" {
		return false
	}
	if value.ProcdService != "" || value.ProcdInstance != "" || len(value.ProcdCommand) != 0 {
		if !token(value.ProcdService, 128) || !token(value.ProcdInstance, 128) || len(value.ProcdCommand) == 0 || len(value.ProcdCommand) > 64 ||
			value.SystemdUnit != "" || value.FragmentPath != "" || value.FragmentSHA256 != "" || value.StartMonotonicUsec != 0 {
			return false
		}
		for index, argument := range value.ProcdCommand {
			if argument == "" || len(argument) > 512 || strings.ContainsAny(argument, "\x00\r\n") || index == 0 && argument != process.Executable {
				return false
			}
		}
		return (value.ProcdUser == "" || token(value.ProcdUser, 128)) && (value.ProcdGroup == "" || token(value.ProcdGroup, 128))
	}
	return token(value.SystemdUnit, 128) && canonicalAuthorityPath(value.FragmentPath) && digest(value.FragmentSHA256) &&
		value.StartMonotonicUsec > 0 && canonicalAuthorityPath(value.ControlGroup)
}

func normalizedAuthorityEndpointIDs(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func canonicalAuthorityPath(value string) bool {
	return strings.HasPrefix(value, "/") && path.Clean(value) == value && value != "/" && len(value) <= 512 && !strings.ContainsAny(value, "\x00\r\n\t")
}

func safeAuthorityProvider(value string) bool {
	return value != "" && len(value) <= 128 && !strings.ContainsAny(value, "\x00\r\n\t")
}

func numeric(value string) bool {
	_, err := strconv.ParseUint(value, 10, 64)
	return value != "" && err == nil
}

// ConfigNodeV1 is backend-native diagnostic evidence. Kind describes the
// provider's representation and is intentionally an open, bounded token; it
// is not part of DesiredPolicyV1 and is not a product-wide artifact catalog.
type ConfigNodeV1 struct {
	ID        string `json:"id"`
	ParentID  string `json:"parentId,omitempty"`
	Kind      string `json:"kind"`
	Order     uint16 `json:"order"`
	Depth     uint8  `json:"depth"`
	Digest    string `json:"digest"`
	Owner     string `json:"owner"`
	ModeClass string `json:"modeClass"`
	Symlink   bool   `json:"symlink"`
}

type MatchContextV1 struct {
	ID             string `json:"id"`
	ConditionClass string `json:"conditionClass"`
	EffectiveHash  string `json:"effectiveHash"`
	Known          bool   `json:"known"`
}

type AuthenticationPostureV1 struct {
	PasswordAuthentication       string   `json:"passwordAuthentication"`
	KbdInteractiveAuthentication string   `json:"kbdInteractiveAuthentication"`
	PermitRootLogin              string   `json:"permitRootLogin"`
	PubkeyAuthentication         string   `json:"pubkeyAuthentication"`
	AuthenticationMethods        []string `json:"authenticationMethods"`
	MaxAuthTries                 uint16   `json:"maxAuthTries"`
	LoginGraceTimeSeconds        uint32   `json:"loginGraceTimeSeconds"`
	MaxStartupsClass             string   `json:"maxStartupsClass"`
}

type ForwardingPostureV1 struct {
	AllowAgentForwarding string `json:"allowAgentForwarding"`
	AllowTCPForwarding   string `json:"allowTcpForwarding"`
	GatewayPorts         string `json:"gatewayPorts"`
	PermitTunnel         string `json:"permitTunnel"`
	X11Forwarding        string `json:"x11Forwarding"`
}

type AuthorizedKeysPostureV1 struct {
	StrictModes          string `json:"strictModes"`
	CommandConfigured    bool   `json:"commandConfigured"`
	PathTemplateCount    uint16 `json:"pathTemplateCount"`
	PathTemplateRevision string `json:"pathTemplateRevision"`
}

type HostKeyPostureV1 struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Count       uint16 `json:"count"`
	Owner       string `json:"owner"`
	ModeClass   string `json:"modeClass"`
	Symlink     bool   `json:"symlink"`
}

type SSHPostureV1 struct {
	Schema                string                               `json:"schema"`
	Binary                BinaryIdentityV1                     `json:"binary"`
	Service               ServiceIdentityV1                    `json:"service"`
	ConfigGraph           []ConfigNodeV1                       `json:"configGraph"`
	MatchContexts         []MatchContextV1                     `json:"matchContexts"`
	Endpoints             []hostresources.ManagementEndpointV1 `json:"endpoints"`
	ListenerAuthorities   []SSHListenerAuthorityV1             `json:"listenerAuthorities,omitempty"`
	Authentication        AuthenticationPostureV1              `json:"authentication"`
	Forwarding            ForwardingPostureV1                  `json:"forwarding"`
	AuthorizedKeys        AuthorizedKeysPostureV1              `json:"authorizedKeys"`
	HostKeys              []HostKeyPostureV1                   `json:"hostKeys"`
	Capabilities          CapabilitySetV1                      `json:"capabilities"`
	ObservedAt            int64                                `json:"observedAt"`
	ExpiresAt             int64                                `json:"expiresAt"`
	SemanticRevision      string                               `json:"semanticRevision"`
	BinaryRevision        string                               `json:"binaryRevision"`
	ServiceRevision       string                               `json:"serviceRevision"`
	ConfigurationRevision string                               `json:"configurationRevision"`
	ReasonCodes           []ReasonCode                         `json:"reasonCodes,omitempty"`
}

func (p SSHPostureV1) Validate(now time.Time) error {
	if p.Schema != PostureSchemaV1 || p.ObservedAt <= 0 || p.ObservedAt > now.UTC().Unix() || p.ExpiresAt <= p.ObservedAt || p.ExpiresAt > p.ObservedAt+int64(MaxPostureLifetime/time.Second) || p.ExpiresAt <= now.UTC().Unix() {
		return NewError("posture", ReasonPostureStale)
	}
	if !digest(p.SemanticRevision) || !digest(p.BinaryRevision) || !digest(p.ServiceRevision) || !digest(p.ConfigurationRevision) || !digest(p.Binary.Digest) || !digest(p.Service.Digest) {
		return NewError("posture", ReasonRevisionMismatch)
	}
	if p.SemanticRevision != PostureSemanticRevision(p) {
		return NewError("posture", ReasonRevisionMismatch)
	}
	if !token(p.Binary.Implementation, 64) || !token(p.Binary.VersionClass, 64) || !p.Binary.Selected ||
		!token(p.Service.Manager, 64) || !token(p.Service.UnitID, 128) || p.Service.State != "active" ||
		p.Binary.Digest != p.BinaryRevision || p.Service.Digest != p.ServiceRevision {
		return NewError("posture", ReasonPostureAmbiguous)
	}
	if len(p.ConfigGraph) == 0 || len(p.ConfigGraph) > 64 || len(p.MatchContexts) == 0 || len(p.MatchContexts) > 64 || len(p.Endpoints) == 0 || len(p.Endpoints) > 32 || len(p.HostKeys) > 32 || len(p.ReasonCodes) > 32 {
		return NewError("posture", ReasonPostureAmbiguous)
	}
	nodes := make(map[string]ConfigNodeV1, len(p.ConfigGraph))
	for index, node := range p.ConfigGraph {
		if !token(node.ID, 128) || !token(node.Kind, 64) || !digest(node.Digest) || !oneOf(node.Owner, "root", "system", "external_managed") || !oneOf(node.ModeClass, "owner_read", "owner_read_write", "system_read") {
			return NewError("posture", ReasonMalformedProviderEvidence)
		}
		if node.Symlink {
			return NewError("posture", ReasonSymlink)
		}
		if _, duplicate := nodes[node.ID]; duplicate || node.Order != uint16(index) || node.Depth > 8 {
			return NewError("posture", ReasonPostureAmbiguous)
		}
		if index == 0 {
			if node.ParentID != "" || node.Depth != 0 {
				return NewError("posture", ReasonPostureAmbiguous)
			}
		} else {
			parent, exists := nodes[node.ParentID]
			if !exists || node.Depth != parent.Depth+1 {
				return NewError("posture", ReasonPostureAmbiguous)
			}
		}
		nodes[node.ID] = node
	}
	for _, context := range p.MatchContexts {
		if !token(context.ID, 128) || !token(context.ConditionClass, 64) || !digest(context.EffectiveHash) || !context.Known {
			return NewError("posture", ReasonUnknownMatchContext)
		}
	}
	endpointIDs := make(map[string]bool, len(p.Endpoints))
	for _, endpoint := range p.Endpoints {
		if endpoint.ServiceKind != hostresources.ManagementSSH || !hostresources.ManagementEndpointCurrent(endpoint, now) ||
			endpoint.ObservedAt > now.UTC().Unix() || endpoint.ConfigurationRevision != p.ConfigurationRevision {
			return NewError("posture", ReasonEndpointAmbiguous)
		}
		if endpointIDs[endpoint.ID] {
			return NewError("posture", ReasonEndpointAmbiguous)
		}
		endpointIDs[endpoint.ID] = true
	}
	if len(p.ListenerAuthorities) > 64 {
		return NewError("posture", ReasonPostureAmbiguous)
	}
	claimedEndpoints := make(map[string]bool, len(endpointIDs))
	for _, authority := range p.ListenerAuthorities {
		if !authority.Valid(now) || authority.BinaryRevision != p.BinaryRevision || authority.ServiceRevision != p.ServiceRevision ||
			authority.ConfigurationRevision != p.ConfigurationRevision {
			return NewError("posture", ReasonMalformedProviderEvidence)
		}
		for _, id := range authority.EndpointIDs {
			endpoint, exists := endpointByID(p.Endpoints, id)
			if !exists || !endpointIDs[id] || claimedEndpoints[id] || !authorityCoversManagementEndpoint(authority, endpoint) {
				return NewError("posture", ReasonEndpointAmbiguous)
			}
			claimedEndpoints[id] = true
		}
	}
	if len(claimedEndpoints) != len(endpointIDs) {
		return NewError("posture", ReasonEndpointAmbiguous)
	}
	for _, key := range p.HostKeys {
		if !token(key.Type, 64) || !digest(key.Fingerprint) || key.Count == 0 || key.Count > 64 || !oneOf(key.Owner, "root", "system") || !oneOf(key.ModeClass, "owner_read", "owner_read_write") {
			return NewError("posture", ReasonMalformedProviderEvidence)
		}
		if key.Symlink {
			return NewError("posture", ReasonSymlink)
		}
	}
	if !oneOf(p.Authentication.PasswordAuthentication, "yes", "no") ||
		!oneOf(p.Authentication.KbdInteractiveAuthentication, "yes", "no") ||
		!oneOf(p.Authentication.PubkeyAuthentication, "yes", "no") ||
		!oneOf(p.Authentication.PermitRootLogin, "yes", "no", "prohibit-password", "without-password", "forced-commands-only") ||
		p.Authentication.MaxAuthTries < 1 || p.Authentication.MaxAuthTries > 20 || p.Authentication.LoginGraceTimeSeconds > 600 ||
		!token(p.Authentication.MaxStartupsClass, 64) || len(p.Authentication.AuthenticationMethods) > 16 {
		return NewError("posture", ReasonPostureAmbiguous)
	}
	for _, method := range p.Authentication.AuthenticationMethods {
		if !token(method, 64) {
			return NewError("posture", ReasonMalformedProviderEvidence)
		}
	}
	if !oneOf(p.Forwarding.AllowAgentForwarding, "yes", "no") ||
		!oneOf(p.Forwarding.AllowTCPForwarding, "yes", "no", "all", "local", "remote") ||
		!oneOf(p.Forwarding.GatewayPorts, "yes", "no", "clientspecified") ||
		!oneOf(p.Forwarding.PermitTunnel, "yes", "no", "point-to-point", "ethernet") ||
		!oneOf(p.Forwarding.X11Forwarding, "yes", "no") ||
		!oneOf(p.AuthorizedKeys.StrictModes, "yes", "no") || p.AuthorizedKeys.PathTemplateCount > 16 ||
		(p.AuthorizedKeys.PathTemplateCount > 0 && !digest(p.AuthorizedKeys.PathTemplateRevision)) ||
		(p.AuthorizedKeys.PathTemplateCount == 0 && p.AuthorizedKeys.PathTemplateRevision != "") {
		return NewError("posture", ReasonPostureAmbiguous)
	}
	if !validCapabilitySet(p.Capabilities) {
		return NewError("posture", ReasonMalformedProviderEvidence)
	}
	for _, reason := range p.ReasonCodes {
		if !token(string(reason), 64) {
			return NewError("posture", ReasonMalformedProviderEvidence)
		}
	}
	if len(p.ReasonCodes) != 0 {
		return NewError("posture", p.ReasonCodes[0])
	}
	return nil
}

func endpointByID(values []hostresources.ManagementEndpointV1, id string) (hostresources.ManagementEndpointV1, bool) {
	for _, value := range values {
		if value.ID == id {
			return value, true
		}
	}
	return hostresources.ManagementEndpointV1{}, false
}

func validCapabilitySet(value CapabilitySetV1) bool {
	for _, availability := range []Availability{value.ObservePosture, value.Prepare, value.Stage, value.Validate, value.Reload, value.Reconnect, value.Rollback} {
		if availability != AvailabilityAvailable && availability != AvailabilityUnavailable && availability != AvailabilityUnknown {
			return false
		}
	}
	if !digest(value.Revision) || len(value.ReasonCodes) > 32 {
		return false
	}
	copy := value
	copy.Revision = ""
	return value.Revision == Revision(copy)
}

type RootLoginPolicy string

const (
	RootLoginUnchanged        RootLoginPolicy = "UNCHANGED"
	RootLoginYes              RootLoginPolicy = "YES"
	RootLoginNo               RootLoginPolicy = "NO"
	RootLoginProhibitPassword RootLoginPolicy = "PROHIBIT_PASSWORD"
)

type DesiredPolicyV1 struct {
	Schema                       string          `json:"schema"`
	MaxAuthTries                 *uint16         `json:"maxAuthTries,omitempty"`
	LoginGraceTimeSeconds        *uint32         `json:"loginGraceTimeSeconds,omitempty"`
	PasswordAuthentication       *bool           `json:"passwordAuthentication,omitempty"`
	KbdInteractiveAuthentication *bool           `json:"kbdInteractiveAuthentication,omitempty"`
	PermitRootLogin              RootLoginPolicy `json:"permitRootLogin"`
	PubkeyAuthentication         *bool           `json:"pubkeyAuthentication,omitempty"`
}

func (p DesiredPolicyV1) Validate() error {
	if p.Schema != PolicySchemaV1 || p.PermitRootLogin != RootLoginUnchanged && p.PermitRootLogin != RootLoginYes && p.PermitRootLogin != RootLoginNo && p.PermitRootLogin != RootLoginProhibitPassword {
		return NewError("policy", ReasonUnsupportedDirective)
	}
	if p.MaxAuthTries != nil && (*p.MaxAuthTries < 1 || *p.MaxAuthTries > 20) || p.LoginGraceTimeSeconds != nil && (*p.LoginGraceTimeSeconds < 1 || *p.LoginGraceTimeSeconds > 600) {
		return NewError("policy", ReasonUnsupportedDirective)
	}
	return nil
}

func (p DesiredPolicyV1) DisablesPasswordPath() bool {
	return p.PasswordAuthentication != nil && !*p.PasswordAuthentication ||
		p.KbdInteractiveAuthentication != nil && !*p.KbdInteractiveAuthentication ||
		p.PermitRootLogin == RootLoginNo || p.PermitRootLogin == RootLoginProhibitPassword
}

type ManagementPreservationPlanV1 struct {
	Schema                    string                               `json:"schema"`
	BeforeEndpoints           []hostresources.ManagementEndpointV1 `json:"beforeEndpoints"`
	AfterEndpoints            []hostresources.ManagementEndpointV1 `json:"afterEndpoints"`
	VerifiedRecoveryPaths     []hostresources.RecoveryPathV1       `json:"verifiedRecoveryPaths"`
	IndependentFailureDomains []string                             `json:"independentFailureDomains"`
	ConsoleVerified           bool                                 `json:"consoleVerified"`
	FreshPubkeyReconnect      bool                                 `json:"freshPubkeyReconnect"`
	WatchdogAvailable         bool                                 `json:"watchdogAvailable"`
	EarliestSafetyExpiry      int64                                `json:"earliestSafetyExpiry"`
	Safe                      bool                                 `json:"safe"`
	ReasonCodes               []ReasonCode                         `json:"reasonCodes,omitempty"`
	Revision                  string                               `json:"revision"`
}

type PreservationInput struct {
	Before   []hostresources.ManagementEndpointV1
	After    []hostresources.ManagementEndpointV1
	Recovery []hostresources.RecoveryPathV1
	Now      time.Time
	Policy   DesiredPolicyV1
	Watchdog bool
}

func BuildPreservationPlan(input PreservationInput) ManagementPreservationPlanV1 {
	now := input.Now.UTC()
	plan := ManagementPreservationPlanV1{Schema: PreservationSchemaV1, WatchdogAvailable: input.Watchdog}
	plan.BeforeEndpoints = currentEndpoints(input.Before, now)
	plan.AfterEndpoints = currentEndpoints(input.After, now)
	afterIDs := make(map[string]bool, len(plan.AfterEndpoints))
	for _, endpoint := range plan.AfterEndpoints {
		afterIDs[endpoint.ID] = true
	}
	domains := map[string]bool{}
	plan.EarliestSafetyExpiry = now.Add(MaxRecoveryLifetime).Unix()
	for _, path := range input.Recovery {
		if !afterIDs[path.EndpointID] || !hostresources.RecoveryPathFresh(path, now) {
			continue
		}
		plan.VerifiedRecoveryPaths = append(plan.VerifiedRecoveryPaths, path)
		domains[path.IndependenceClass] = true
		if path.ExpiresAt < plan.EarliestSafetyExpiry {
			plan.EarliestSafetyExpiry = path.ExpiresAt
		}
		if path.VerificationMethod == "provider_console" {
			plan.ConsoleVerified = true
		}
		if path.VerificationMethod == "fresh_ssh_login" && path.OperationBound && path.SingleUse {
			plan.FreshPubkeyReconnect = true
		}
	}
	for domain := range domains {
		plan.IndependentFailureDomains = append(plan.IndependentFailureDomains, domain)
	}
	sort.Strings(plan.IndependentFailureDomains)
	if len(plan.AfterEndpoints) == 0 {
		plan.ReasonCodes = append(plan.ReasonCodes, ReasonManagementPathRemoved)
	}
	if len(plan.VerifiedRecoveryPaths) == 0 {
		plan.ReasonCodes = append(plan.ReasonCodes, ReasonRecoveryPathMissing)
	}
	if !plan.WatchdogAvailable {
		plan.ReasonCodes = append(plan.ReasonCodes, ReasonWatchdogMissing)
	}
	if input.Policy.DisablesPasswordPath() {
		if !plan.ConsoleVerified {
			plan.ReasonCodes = append(plan.ReasonCodes, ReasonConsoleMissing)
		}
		if !plan.FreshPubkeyReconnect {
			plan.ReasonCodes = append(plan.ReasonCodes, ReasonFreshPubkeyMissing)
		}
	}
	if plan.EarliestSafetyExpiry <= now.Add(time.Minute).Unix() {
		plan.ReasonCodes = append(plan.ReasonCodes, ReasonExpiryTooNear)
	}
	plan.ReasonCodes = uniqueReasons(plan.ReasonCodes)
	plan.Safe = len(plan.ReasonCodes) == 0
	plan.Revision = Revision(struct {
		Schema                          string
		Before                          []hostresources.ManagementEndpointV1
		After                           []hostresources.ManagementEndpointV1
		Paths                           []hostresources.RecoveryPathV1
		Domains                         []string
		Console, Pubkey, Watchdog, Safe bool
		Expiry                          int64
		Reasons                         []ReasonCode
	}{plan.Schema, plan.BeforeEndpoints, plan.AfterEndpoints, plan.VerifiedRecoveryPaths, plan.IndependentFailureDomains, plan.ConsoleVerified, plan.FreshPubkeyReconnect, plan.WatchdogAvailable, plan.Safe, plan.EarliestSafetyExpiry, plan.ReasonCodes})
	return plan
}

type CandidateState string

const (
	StateDraft                  CandidateState = "DRAFT"
	StatePreflighted            CandidateState = "PREFLIGHTED"
	StateStaged                 CandidateState = "STAGED"
	StateValidated              CandidateState = "VALIDATED"
	StateReloadPending          CandidateState = "RELOAD_PENDING"
	StateReconnectRequired      CandidateState = "RECONNECT_REQUIRED"
	StateCommitted              CandidateState = "COMMITTED"
	StateRollbackPending        CandidateState = "ROLLBACK_PENDING"
	StateRolledBack             CandidateState = "ROLLED_BACK"
	StateManualRecoveryRequired CandidateState = "MANUAL_RECOVERY_REQUIRED"
)

func (s CandidateState) Terminal() bool {
	return s == StateCommitted || s == StateRolledBack || s == StateManualRecoveryRequired
}

func TransitionAllowed(from, to CandidateState) bool {
	allowed := map[CandidateState][]CandidateState{
		StateDraft: {StatePreflighted, StateManualRecoveryRequired}, StatePreflighted: {StateStaged, StateManualRecoveryRequired},
		StateStaged:            {StateValidated, StateRollbackPending, StateManualRecoveryRequired},
		StateValidated:         {StateReloadPending, StateRollbackPending, StateManualRecoveryRequired},
		StateReloadPending:     {StateReconnectRequired, StateRollbackPending, StateManualRecoveryRequired},
		StateReconnectRequired: {StateCommitted, StateRollbackPending, StateManualRecoveryRequired},
		StateRollbackPending:   {StateRolledBack, StateManualRecoveryRequired},
	}
	for _, candidate := range allowed[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

type CandidateV1 struct {
	Schema                string                       `json:"schema"`
	OperationID           string                       `json:"operationId"`
	IdempotencyKey        string                       `json:"idempotencyKey"`
	EndpointID            string                       `json:"endpointId"`
	State                 CandidateState               `json:"state"`
	Revision              uint64                       `json:"revision"`
	Policy                DesiredPolicyV1              `json:"policy"`
	Preservation          ManagementPreservationPlanV1 `json:"preservation"`
	CandidateDigest       string                       `json:"candidateDigest"`
	BindingDigest         string                       `json:"bindingDigest"`
	BeforeArtifactDigest  string                       `json:"beforeArtifactDigest"`
	AfterArtifactDigest   string                       `json:"afterArtifactDigest"`
	PostureRevision       string                       `json:"postureRevision"`
	EndpointRevision      string                       `json:"endpointRevision"`
	RecoveryRevision      string                       `json:"recoveryRevision"`
	ProviderRevision      string                       `json:"providerRevision"`
	BinaryRevision        string                       `json:"binaryRevision"`
	ServiceRevision       string                       `json:"serviceRevision"`
	ConfigurationRevision string                       `json:"configurationRevision"`
	EarliestSafetyExpiry  int64                        `json:"earliestSafetyExpiry"`
	ReconnectExpiresAt    int64                        `json:"reconnectExpiresAt,omitempty"`
	RollbackAttempts      uint8                        `json:"rollbackAttempts"`
	BrokerStageReleased   bool                         `json:"brokerStageReleased"`
	RestoredUntrusted     bool                         `json:"restoredUntrusted"`
	ReconciledAt          int64                        `json:"reconciledAt"`
	CreatedAt             int64                        `json:"createdAt"`
	UpdatedAt             int64                        `json:"updatedAt"`
	ReasonCodes           []ReasonCode                 `json:"reasonCodes,omitempty"`
}

type ReconnectChallengeV1 struct {
	Schema                string `json:"schema"`
	OperationID           string `json:"operationId"`
	CandidateDigest       string `json:"candidateDigest"`
	MarkerDigest          string `json:"markerDigest"`
	EndpointID            string `json:"endpointId"`
	PrincipalID           string `json:"principalId"`
	AuthenticationClass   string `json:"authenticationClass"`
	ServiceRevision       string `json:"serviceRevision"`
	BinaryRevision        string `json:"binaryRevision"`
	ConfigurationRevision string `json:"configurationRevision"`
	VerifierDigest        string `json:"-"`
	IssuedAt              int64  `json:"issuedAt"`
	ExpiresAt             int64  `json:"expiresAt"`
	ConsumedAt            int64  `json:"consumedAt,omitempty"`
	Revision              uint64 `json:"revision"`
}

func Revision(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// PostureSemanticRevision excludes collection time and expiry while retaining
// every value that changes the meaning or safety of the posture.
func PostureSemanticRevision(posture SSHPostureV1) string {
	copy := posture
	copy.SemanticRevision = ""
	copy.ObservedAt, copy.ExpiresAt = 0, 0
	copy.Endpoints = append([]hostresources.ManagementEndpointV1(nil), posture.Endpoints...)
	for index := range copy.Endpoints {
		copy.Endpoints[index].ObservedAt = 0
		copy.Endpoints[index].ExpiresAt = 0
	}
	copy.ListenerAuthorities = append([]SSHListenerAuthorityV1(nil), posture.ListenerAuthorities...)
	for index := range copy.ListenerAuthorities {
		copy.ListenerAuthorities[index].ObservedAt = 0
		copy.ListenerAuthorities[index].ExpiresAt = 0
	}
	return Revision(copy)
}

func BindingDigest(candidate CandidateV1) string {
	if candidate.EndpointID == "" {
		return Revision(struct {
			Schema, Operation, Candidate, Posture, Endpoint, Recovery, Provider, Binary, Service, Configuration, Preservation string
			Expiry                                                                                                            int64
		}{CandidateSchemaV1, candidate.OperationID, candidate.CandidateDigest, candidate.PostureRevision, candidate.EndpointRevision,
			candidate.RecoveryRevision, candidate.ProviderRevision, candidate.BinaryRevision, candidate.ServiceRevision,
			candidate.ConfigurationRevision, candidate.Preservation.Revision, candidate.EarliestSafetyExpiry})
	}
	return Revision(struct {
		Schema, Operation, ManagedEndpoint, Candidate, Posture, Endpoint, Recovery, Provider, Binary, Service, Configuration, Preservation string
		Expiry                                                                                                                             int64
	}{CandidateSchemaV1, candidate.OperationID, candidate.EndpointID, candidate.CandidateDigest, candidate.PostureRevision, candidate.EndpointRevision,
		candidate.RecoveryRevision, candidate.ProviderRevision, candidate.BinaryRevision, candidate.ServiceRevision,
		candidate.ConfigurationRevision, candidate.Preservation.Revision, candidate.EarliestSafetyExpiry})
}

func currentEndpoints(values []hostresources.ManagementEndpointV1, now time.Time) []hostresources.ManagementEndpointV1 {
	result := make([]hostresources.ManagementEndpointV1, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if hostresources.ManagementEndpointCurrent(value, now) && !seen[value.ID] {
			seen[value.ID] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func uniqueReasons(values []ReasonCode) []ReasonCode {
	seen := map[ReasonCode]bool{}
	result := make([]ReasonCode, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func digest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func token(value string, limit int) bool {
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

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
