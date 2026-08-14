package resources

import (
	"net/netip"
	"strings"
	"time"
)

const (
	ManagementEndpointSchemaV1 = "solovey-ui/management-endpoint/v1"
	RecoveryPathSchemaV1       = "solovey-ui/recovery-path/v1"
)

type ManagementServiceKind string

const (
	ManagementPanel             ManagementServiceKind = "PANEL"
	ManagementSSH               ManagementServiceKind = "SSH"
	ManagementSubscriptionAdmin ManagementServiceKind = "SUBSCRIPTION_ADMIN"
	ManagementOtherAdmin        ManagementServiceKind = "OTHER_ADMIN"
)

type ManagementEndpointV1 struct {
	Schema                string                `json:"schema"`
	ID                    string                `json:"id"`
	Network               Network               `json:"network"`
	Family                AddressFamily         `json:"family"`
	Bind                  string                `json:"bind"`
	Port                  uint16                `json:"port"`
	ServiceKind           ManagementServiceKind `json:"serviceKind"`
	Exposure              EndpointIntent        `json:"exposure"`
	Owner                 string                `json:"owner"`
	OwnerRevision         string                `json:"ownerRevision,omitempty"`
	ResourceID            string                `json:"resourceId,omitempty"`
	Purpose               string                `json:"purpose,omitempty"`
	RecoveryPolicy        string                `json:"recoveryPolicy"`
	Source                string                `json:"source"`
	ConfiguredIntent      bool                  `json:"configuredIntent"`
	ObservedListener      bool                  `json:"observedListener"`
	Wildcard              bool                  `json:"wildcard"`
	DualStack             bool                  `json:"dualStack"`
	ConfidenceBP          int                   `json:"confidenceBp"`
	ObservedAt            int64                 `json:"observedAt"`
	ExpiresAt             int64                 `json:"expiresAt,omitempty"`
	ConfigurationRevision string                `json:"configurationRevision"`
	RuntimeRevision       string                `json:"runtimeRevision,omitempty"`
	SemanticRevision      string                `json:"semanticRevision,omitempty"`
	ReasonCodes           []string              `json:"reasonCodes,omitempty"`
}

type RecoveryPathV1 struct {
	Schema                string   `json:"schema"`
	ID                    string   `json:"id"`
	Kind                  string   `json:"kind"`
	EndpointID            string   `json:"endpointId"`
	PrincipalID           string   `json:"principalId"`
	SourcePrefix          string   `json:"sourcePrefix,omitempty"`
	VerificationMethod    string   `json:"verificationMethod"`
	EvidenceProvider      string   `json:"evidenceProvider,omitempty"`
	TargetOperation       string   `json:"targetOperation,omitempty"`
	VerifiedAt            int64    `json:"verifiedAt"`
	ExpiresAt             int64    `json:"expiresAt"`
	IndependenceClass     string   `json:"independenceClass"`
	VerificationState     string   `json:"verificationState"`
	OperationBound        bool     `json:"operationBound"`
	SingleUse             bool     `json:"singleUse"`
	ConsumedAt            int64    `json:"consumedAt,omitempty"`
	Revision              uint64   `json:"revision,omitempty"`
	ReasonCodes           []string `json:"reasonCodes,omitempty"`
	SourceRevision        string   `json:"sourceRevision"`
	ConfigurationRevision string   `json:"configurationRevision"`
	ServiceRevision       string   `json:"serviceRevision,omitempty"`
	BinaryRevision        string   `json:"binaryRevision,omitempty"`
	ProducerRevision      string   `json:"producerRevision"`
}

func ManagementEndpointFromResource(resource ProtectableResource, kind ManagementServiceKind, now time.Time) ManagementEndpointV1 {
	endpoint := PublicEndpoint{}
	if len(resource.Endpoints) > 0 {
		endpoint = resource.Endpoints[0]
	} else {
		endpoint = BuildEndpointFact(resource, NetworkForProtocol(resource.Protocol), now)
	}
	reasons := append([]string(nil), endpoint.ReasonCodes...)
	if endpoint.Key.Port == 0 {
		reasons = append(reasons, "management_endpoint_unknown")
	}
	return ManagementEndpointV1{
		Schema: ManagementEndpointSchemaV1, ID: "management:" + strings.TrimPrefix(resource.ID, "core:"),
		Network: endpoint.Key.Network, Family: endpoint.Key.AddressFamily, Bind: endpoint.Key.BindAddress,
		Port: endpoint.Key.Port, ServiceKind: kind, Exposure: endpoint.Intent, Owner: resource.Owner,
		ResourceID: resource.ID, RecoveryPolicy: "fresh_independent_path_required", Source: endpoint.Source,
		Purpose: "administrative_access", ConfiguredIntent: true,
		ConfidenceBP: endpoint.ConfidenceBP, ObservedAt: endpoint.ObservedAt, ExpiresAt: now.UTC().Add(90 * time.Second).Unix(),
		ConfigurationRevision: endpoint.ConfigurationRevision, SemanticRevision: endpoint.ConfigurationRevision,
		ReasonCodes: normalizedReasonCodes(reasons),
	}
}

// ManagementEndpointCurrent is the neutral fail-closed validity fence shared
// by recovery-path evaluation and every mutating policy consumer.
func ManagementEndpointCurrent(value ManagementEndpointV1, now time.Time) bool {
	now = now.UTC()
	if now.IsZero() || value.Schema != ManagementEndpointSchemaV1 || !safeManagementID(value.ID) ||
		(value.Network != NetworkTCP && value.Network != NetworkUDP) ||
		(value.Family != AddressFamilyIPv4 && value.Family != AddressFamilyIPv6) || value.Port == 0 ||
		value.ObservedAt <= 0 || value.ObservedAt > now.Add(5*time.Minute).Unix() || value.ExpiresAt <= now.Unix() ||
		value.ExpiresAt <= value.ObservedAt || value.ExpiresAt > value.ObservedAt+300 ||
		value.ConfidenceBP <= 0 || value.ConfidenceBP > 10000 || !validManagementRevision(value.ConfigurationRevision) ||
		!validManagementKind(string(value.ServiceKind)) || !safeManagementID(value.Owner) || !safeManagementID(value.Source) ||
		(value.ResourceID != "" && !safeManagementID(value.ResourceID)) ||
		(value.Purpose != "" && !safeManagementID(value.Purpose)) || !safeManagementID(value.RecoveryPolicy) {
		return false
	}
	normalized := NormalizeListen(value.Bind)
	if normalized.Value != value.Bind || AddressFamilyForListen(value.Bind) != value.Family ||
		value.Exposure != EndpointIntentForBind(value.Bind) {
		return false
	}
	for _, revision := range []string{value.OwnerRevision, value.RuntimeRevision, value.SemanticRevision} {
		if revision != "" && !validManagementRevision(revision) {
			return false
		}
	}
	if !value.ConfiguredIntent && !value.ObservedListener ||
		value.ServiceKind == ManagementSSH && value.ConfiguredIntent == value.ObservedListener {
		// An SSH endpoint is one configured intent or one observed listener.
		// Collapsing both facts (or carrying neither) hides configuration/runtime
		// disagreement and is unsafe for management-preservation decisions.
		return false
	}
	for _, reason := range value.ReasonCodes {
		lower := strings.ToLower(strings.TrimSpace(reason))
		if !safeManagementID(lower) || len(lower) > 64 {
			return false
		}
		if strings.Contains(lower, "unknown") || strings.Contains(lower, "stale") || strings.Contains(lower, "truncated") || strings.Contains(lower, "ambiguous") || strings.Contains(lower, "invalid") || strings.Contains(lower, "unavailable") || strings.Contains(lower, "not_verified") {
			return false
		}
	}
	return true
}

// RecoveryPathFresh accepts only independently verified, unexpired evidence.
// An existing connection is intentionally never a verification method.
func RecoveryPathFresh(value RecoveryPathV1, now time.Time) bool {
	nowUnix := now.UTC().Unix()
	if !RecoveryPathValid(value, now) || !strings.EqualFold(strings.TrimSpace(value.VerificationState), "verified") || value.ExpiresAt <= nowUnix || value.ConsumedAt != 0 || len(value.ReasonCodes) != 0 {
		return false
	}
	independence := strings.ToLower(strings.TrimSpace(value.IndependenceClass))
	switch strings.ToLower(strings.TrimSpace(value.VerificationMethod)) {
	case "fresh_panel_login":
		return independence == "independent" || independence == "independent_reconnect"
	case "fresh_ssh_login":
		return value.OperationBound && value.SingleUse && strings.TrimSpace(value.TargetOperation) != "" &&
			(independence == "independent" || independence == "independent_reconnect")
	case "provider_console":
		return value.OperationBound && strings.TrimSpace(value.EvidenceProvider) != "" &&
			(independence == "independent" || independence == "provider_control_plane")
	default:
		return false
	}
}

func RecoveryPathValid(value RecoveryPathV1, now time.Time) bool {
	nowUnix := now.UTC().Unix()
	if value.Schema != RecoveryPathSchemaV1 || !safeManagementID(value.ID) || !safeManagementID(value.EndpointID) || !safeManagementID(value.PrincipalID) || !validManagementKind(value.Kind) || !safeManagementID(value.VerificationMethod) || !safeManagementID(value.IndependenceClass) || !safeManagementID(value.VerificationState) || !validManagementRevision(value.SourceRevision) || !validManagementRevision(value.ConfigurationRevision) || value.ProducerRevision != "" && !validManagementRevision(value.ProducerRevision) || value.VerifiedAt <= 0 || value.VerifiedAt > nowUnix+300 || value.ExpiresAt <= value.VerifiedAt || value.ExpiresAt > value.VerifiedAt+int64((24*time.Hour)/time.Second) || len(value.ReasonCodes) > 16 {
		return false
	}
	for _, token := range []string{value.EvidenceProvider, value.TargetOperation} {
		if token != "" && !safeManagementID(token) {
			return false
		}
	}
	for _, revision := range []string{value.ServiceRevision, value.BinaryRevision} {
		if revision != "" && !validManagementRevision(revision) {
			return false
		}
	}
	if (value.OperationBound || value.SingleUse) && (value.ExpiresAt-value.VerifiedAt > int64((15*time.Minute)/time.Second) || value.Revision == 0) {
		return false
	}
	if value.ConsumedAt < 0 || value.ConsumedAt > 0 && (value.ConsumedAt < value.VerifiedAt || value.ConsumedAt > nowUnix+300) {
		return false
	}
	if prefix := strings.TrimSpace(value.SourcePrefix); prefix != "" {
		parsed, err := netip.ParsePrefix(prefix)
		if err != nil || parsed.Masked().String() != prefix {
			return false
		}
	}
	for _, reason := range value.ReasonCodes {
		if !safeManagementID(reason) || len(reason) > 64 {
			return false
		}
	}
	return true
}

func validManagementRevision(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			if r < 'a' || r > 'f' {
				return false
			}
		}
	}
	return true
}

func validManagementKind(value string) bool {
	switch ManagementServiceKind(strings.ToUpper(strings.TrimSpace(value))) {
	case ManagementPanel, ManagementSSH, ManagementSubscriptionAdmin, ManagementOtherAdmin:
		return true
	default:
		return false
	}
}

func safeManagementID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}
