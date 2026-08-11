package interception

import (
	"context"
	"errors"
	"runtime"
	"sort"
	"strings"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	StatusSchemaV1 = "solovey-ui/server-protection/interception-status/v1"
	PlanSchemaV1   = "solovey-ui/server-protection/interception-plan/v1"

	DispositionShip                     DispositionV1 = "SHIP"
	DispositionInspectionOnly           DispositionV1 = "INSPECTION_ONLY"
	DispositionBlockedMissingCapability DispositionV1 = "BLOCKED_MISSING_CAPABILITY"
	DispositionNotShipped               DispositionV1 = "NOT_SHIPPED"
	DispositionExternalManaged          DispositionV1 = "EXTERNAL_MANAGED"

	CodeMalformedInput       = "INTERCEPTION_MALFORMED_INPUT"
	CodeFactMissing          = "INTERCEPTION_FACT_MISSING_OR_STALE"
	CodeMutationNotShipped   = "INTERCEPTION_MUTATION_NOT_SHIPPED"
	CodeOperationUnavailable = "INTERCEPTION_OPERATION_UNAVAILABLE"
	CodeInternalFailure      = "INTERCEPTION_INTERNAL_FAILURE"

	mutationConfirmation = "I UNDERSTAND FORWARDED INTERCEPTION IS NOT SHIPPED"
)

type DispositionV1 string

type ProfileDispositionV1 struct {
	Kind          hostresources.InterceptionKindV1 `json:"kind"`
	Network       hostresources.Network            `json:"network"`
	AddressFamily hostresources.AddressFamily      `json:"addressFamily"`
	Disposition   DispositionV1                    `json:"disposition"`
	ReasonCodes   []string                         `json:"reasonCodes"`
}

type ResourceStatusV1 struct {
	Fact        hostresources.InterceptionInboundFactV1 `json:"fact"`
	Reference   hostresources.InterceptionReferenceV1   `json:"reference"`
	Disposition DispositionV1                           `json:"disposition"`
	ReasonCodes []string                                `json:"reasonCodes"`
}

type StatusV1 struct {
	Schema               string                                      `json:"schema"`
	GeneratedAt          int64                                       `json:"generatedAt"`
	ArchitectureRevision string                                      `json:"architectureRevision"`
	Experimental         bool                                        `json:"experimental"`
	DefaultEnabled       bool                                        `json:"defaultEnabled"`
	MutationAvailable    bool                                        `json:"mutationAvailable"`
	ForwardedIngressOnly bool                                        `json:"forwardedIngressOnly"`
	LocalOutputShipped   bool                                        `json:"localOutputShipped"`
	TUNAdoptionShipped   bool                                        `json:"tunAdoptionShipped"`
	AllocatorState       string                                      `json:"allocatorState"`
	HelperState          string                                      `json:"helperState"`
	HealthState          string                                      `json:"healthState"`
	Resources            []ResourceStatusV1                          `json:"resources"`
	IngressScopes        []hostresources.ForwardedIngressScopeFactV1 `json:"ingressScopes"`
	ProfileMatrix        []ProfileDispositionV1                      `json:"profileMatrix"`
	GlobalReasonCodes    []string                                    `json:"globalReasonCodes"`
}

type PreviewRequestV1 struct {
	Interception hostresources.InterceptionReferenceV1 `json:"interception"`
}

type PlanV1 struct {
	Schema                string                                           `json:"schema"`
	PlanID                string                                           `json:"planId"`
	PlanRevision          string                                           `json:"planRevision"`
	GeneratedAt           int64                                            `json:"generatedAt"`
	ExpiresAt             int64                                            `json:"expiresAt"`
	Interception          hostresources.InterceptionReferenceV1            `json:"interception"`
	Fact                  hostresources.InterceptionInboundFactV1          `json:"fact"`
	EligibleIngressScopes []hostresources.ForwardedIngressScopeReferenceV1 `json:"eligibleIngressScopes"`
	Disposition           DispositionV1                                    `json:"disposition"`
	DesiredState          string                                           `json:"desiredState"`
	SelectedState         string                                           `json:"selectedState"`
	ActualState           string                                           `json:"actualState"`
	AllocatorState        string                                           `json:"allocatorState"`
	ManagedMark           *uint32                                          `json:"managedMark"`
	ManagedMask           *uint32                                          `json:"managedMask"`
	RoutingTable          *uint32                                          `json:"routingTable"`
	RulePriority          *uint32                                          `json:"rulePriority"`
	ReasonCodes           []string                                         `json:"reasonCodes"`
}

type BlockedMutationRequestV1 struct {
	PlanID           string `json:"planId"`
	ExpectedRevision string `json:"expectedRevision"`
	OperationID      string `json:"operationId,omitempty"`
	IdempotencyKey   string `json:"idempotencyKey"`
	Confirmation     string `json:"confirmation"`
}

type OperationStatusV1 struct {
	OperationID      string   `json:"operationId"`
	State            string   `json:"state"`
	RecoveryRequired bool     `json:"recoveryRequired"`
	ReasonCodes      []string `json:"reasonCodes"`
}

type Service struct {
	Interceptions *hostresources.InterceptionRegistryV1
	IngressScopes *hostresources.ForwardedIngressScopeRegistryV1
	Health        *componenthealth.InterceptionProbeRegistryV1
	Now           func() time.Time
	GOOS          string
}

func New() *Service {
	return &Service{
		Interceptions: hostresources.DefaultInterceptionsV1,
		IngressScopes: hostresources.DefaultIngressScopesV1,
		Health:        componenthealth.DefaultInterceptionProbesV1,
		Now:           time.Now, GOOS: runtime.GOOS,
	}
}

func (s *Service) currentTime() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) Status(ctx context.Context) (StatusV1, error) {
	now := s.currentTime()
	status := StatusV1{
		Schema: StatusSchemaV1, GeneratedAt: now.Unix(), ArchitectureRevision: architectureRevisionV1(),
		Experimental: true, DefaultEnabled: false, MutationAvailable: false, ForwardedIngressOnly: true,
		LocalOutputShipped: false, TUNAdoptionShipped: false,
		AllocatorState: "NOT_ALLOCATED_NON_ACTIONABLE", HelperState: "TYPED_INTERCEPTION_MUTATION_NOT_SHIPPED",
		HealthState: "FORWARDED_TRAFFIC_PROBE_PROVIDER_ABSENT", ProfileMatrix: profileMatrixV1(),
		GlobalReasonCodes: []string{
			"FORWARDED_INGRESS_AUTHORITY_NOT_SHIPPED",
			"KERNEL_INTERCEPTION_CAPABILITY_NOT_PROVEN",
			"MANAGEMENT_EXCLUSION_AUTHORITY_NOT_SHIPPED",
			"POST_MARKER_FORWARDED_TRAFFIC_PROBE_PROVIDER_ABSENT",
			"TYPED_INTERCEPTION_MUTATION_NOT_SHIPPED",
		},
	}
	if s != nil && s.Health != nil && s.Health.ProviderCount() > 0 {
		status.HealthState = "PROVIDER_REGISTERED_MUTATION_STILL_NOT_SHIPPED"
		status.GlobalReasonCodes = withoutReason(status.GlobalReasonCodes, "POST_MARKER_FORWARDED_TRAFFIC_PROBE_PROVIDER_ABSENT")
	}
	if s == nil || s.Interceptions == nil || s.IngressScopes == nil {
		return status, errors.New(CodeInternalFailure)
	}
	facts, factErr := s.Interceptions.FactsV1(ctx, now)
	if factErr != nil && factErr.Error() != "interception_provider_v1_absent" {
		return status, errors.New(CodeInternalFailure)
	}
	scopes, scopeErr := s.IngressScopes.FactsV1(ctx, now)
	if scopeErr != nil && scopeErr.Error() != "forwarded_ingress_provider_v1_absent" {
		return status, errors.New(CodeInternalFailure)
	}
	status.IngressScopes = scopes
	status.Resources = make([]ResourceStatusV1, 0, len(facts))
	for _, fact := range facts {
		disposition, reasons := s.disposition(fact, scopes)
		reference, referenceErr := hostresources.ReferenceInterceptionV1(fact, now)
		if referenceErr != nil {
			return status, errors.New(CodeInternalFailure)
		}
		status.Resources = append(status.Resources, ResourceStatusV1{Fact: fact, Reference: reference, Disposition: disposition, ReasonCodes: reasons})
	}
	sort.Slice(status.Resources, func(i, j int) bool {
		return status.Resources[i].Fact.ResourceID+"\x00"+status.Resources[i].Fact.EndpointID <
			status.Resources[j].Fact.ResourceID+"\x00"+status.Resources[j].Fact.EndpointID
	})
	return status, nil
}

func (s *Service) Preview(ctx context.Context, request PreviewRequestV1) (PlanV1, error) {
	if request.Interception.Validate() != nil {
		return PlanV1{}, errors.New(CodeMalformedInput)
	}
	now := s.currentTime()
	if s == nil || s.Interceptions == nil || s.IngressScopes == nil {
		return PlanV1{}, errors.New(CodeInternalFailure)
	}
	fact, err := s.Interceptions.ResolveV1(ctx, request.Interception, now)
	if err != nil {
		return PlanV1{}, errors.New(CodeFactMissing)
	}
	scopes, err := s.IngressScopes.FactsV1(ctx, now)
	if err != nil {
		scopes = nil
	}
	disposition, reasons := s.disposition(fact, scopes)
	eligible := make([]hostresources.ForwardedIngressScopeReferenceV1, 0)
	for _, scope := range scopes {
		if scope.AddressFamily != fact.AddressFamily || !scope.ForwardedIngress ||
			scope.Ownership != hostresources.IngressScopeProviderManagedV1 {
			continue
		}
		reference, referenceErr := hostresources.ReferenceIngressScopeV1(scope, now)
		if referenceErr == nil {
			eligible = append(eligible, reference)
		}
	}
	plan := PlanV1{
		Schema: PlanSchemaV1, GeneratedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
		Interception: request.Interception, Fact: fact, EligibleIngressScopes: eligible,
		Disposition: disposition, DesiredState: "DISABLED", SelectedState: "NONE",
		ActualState: "NOT_APPLIED", AllocatorState: "NOT_ALLOCATED_NON_ACTIONABLE",
		ReasonCodes: reasons,
	}
	plan.PlanRevision = hostresources.Revision(planRevisionInputV1(plan))
	plan.PlanID = "interception-plan:" + plan.PlanRevision[:32]
	return plan, nil
}

func (s *Service) BlockedMutation(request BlockedMutationRequestV1) error {
	if !safeToken(request.PlanID, 128) || !digest(request.ExpectedRevision) ||
		!safeToken(request.IdempotencyKey, 128) || request.Confirmation != mutationConfirmation {
		return errors.New(CodeMalformedInput)
	}
	return errors.New(CodeMutationNotShipped)
}

func (*Service) Operation(operationID string) (OperationStatusV1, error) {
	if !safeToken(operationID, 128) {
		return OperationStatusV1{}, errors.New(CodeMalformedInput)
	}
	return OperationStatusV1{}, errors.New(CodeOperationUnavailable)
}

func (s *Service) disposition(fact hostresources.InterceptionInboundFactV1, scopes []hostresources.ForwardedIngressScopeFactV1) (DispositionV1, []string) {
	reasons := append([]string(nil), fact.ReasonCodes...)
	if fact.Kind == hostresources.InterceptionTUNV1 || fact.TUNOwned || fact.LocalOutputCapture {
		return DispositionNotShipped, normalizeReasons(append(reasons, "TUN_AND_LOCAL_OUTPUT_NOT_SHIPPED"))
	}
	if fact.Ownership == hostresources.InterceptionExternalManagedV1 {
		return DispositionExternalManaged, normalizeReasons(append(reasons, "EXTERNAL_MANAGED_LISTENER"))
	}
	if s.GOOS != "linux" {
		reasons = append(reasons, "LINUX_PLATFORM_REQUIRED")
	}
	if !fact.RuntimeReady {
		reasons = append(reasons, "RUNTIME_NOT_PROVEN")
	}
	if fact.Ownership != hostresources.InterceptionProviderManagedV1 ||
		fact.ListenerState != hostresources.InterceptionListenerObservedExactV1 {
		reasons = append(reasons, "EXACT_PROVIDER_OWNED_LISTENER_REQUIRED")
	}
	if !fact.OriginalDestinationPreserved {
		reasons = append(reasons, "ORIGINAL_DESTINATION_NOT_PROVEN")
	}
	if !fact.SourcePreserved {
		reasons = append(reasons, "SOURCE_PRESERVATION_NOT_PROVEN")
	}
	scopeFound := false
	for _, scope := range scopes {
		if scope.AddressFamily == fact.AddressFamily && scope.ForwardedIngress &&
			scope.Ownership == hostresources.IngressScopeProviderManagedV1 &&
			!scope.Loopback && !scope.Virtual && !scope.Management && !scope.ExternalManaged {
			scopeFound = true
			break
		}
	}
	if !scopeFound {
		reasons = append(reasons, "EXACT_PROVIDER_OWNED_FORWARDED_INGRESS_SCOPE_REQUIRED")
	}
	reasons = append(reasons,
		"KERNEL_INTERCEPTION_CAPABILITY_NOT_PROVEN",
		"MANAGEMENT_EXCLUSION_AUTHORITY_NOT_SHIPPED",
		"TYPED_INTERCEPTION_MUTATION_NOT_SHIPPED",
	)
	if s.Health == nil || s.Health.ProviderCount() == 0 {
		reasons = append(reasons, "POST_MARKER_FORWARDED_TRAFFIC_PROBE_PROVIDER_ABSENT")
	}
	if fact.Kind == hostresources.InterceptionTProxyV1 {
		reasons = append(reasons, "POLICY_ROUTING_ALLOCATOR_NOT_SHIPPED", "ROUTE_RULE_INSPECTION_NOT_SHIPPED")
	}
	return DispositionBlockedMissingCapability, normalizeReasons(reasons)
}

func profileMatrixV1() []ProfileDispositionV1 {
	result := make([]ProfileDispositionV1, 0, 12)
	for _, family := range []hostresources.AddressFamily{hostresources.AddressFamilyIPv4, hostresources.AddressFamilyIPv6} {
		result = append(result,
			ProfileDispositionV1{Kind: hostresources.InterceptionRedirectV1, Network: hostresources.NetworkTCP, AddressFamily: family,
				Disposition: DispositionBlockedMissingCapability,
				ReasonCodes: []string{"REQUIRES_EXACT_CURRENT_FACTS_AND_MISSING_PLATFORM_PROOFS"}},
			ProfileDispositionV1{Kind: hostresources.InterceptionRedirectV1, Network: hostresources.NetworkUDP, AddressFamily: family,
				Disposition: DispositionNotShipped, ReasonCodes: []string{"PINNED_REDIRECT_IS_TCP_ONLY"}},
			ProfileDispositionV1{Kind: hostresources.InterceptionTProxyV1, Network: hostresources.NetworkTCP, AddressFamily: family,
				Disposition: DispositionBlockedMissingCapability,
				ReasonCodes: []string{"REQUIRES_EXACT_CURRENT_FACTS_AND_MISSING_PLATFORM_PROOFS"}},
			ProfileDispositionV1{Kind: hostresources.InterceptionTProxyV1, Network: hostresources.NetworkUDP, AddressFamily: family,
				Disposition: DispositionBlockedMissingCapability,
				ReasonCodes: []string{"REQUIRES_EXACT_CURRENT_FACTS_AND_MISSING_PLATFORM_PROOFS"}},
		)
	}
	return result
}

func architectureRevisionV1() string {
	return hostresources.Revision(struct {
		Schema, Core, Ingress, Mutation, Health string
	}{
		StatusSchemaV1, hostresources.InterceptionProviderRevisionV1, hostresources.IngressScopeProviderRevisionV1,
		"not-shipped", "not-shipped",
	})
}

func planRevisionInputV1(plan PlanV1) PlanV1 {
	plan.PlanID, plan.PlanRevision = "", ""
	plan.GeneratedAt, plan.ExpiresAt = 0, 0
	return plan
}

func normalizeReasons(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if safeToken(value, 96) && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	if len(result) > 32 {
		result = result[:32]
	}
	return result
}

func withoutReason(values []string, remove string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != remove {
			result = append(result, value)
		}
	}
	return result
}

func safeToken(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("._:@+-", r) {
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
	for _, r := range value {
		if r < '0' || r > '9' && r < 'a' || r > 'f' {
			return false
		}
	}
	return true
}

func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	switch err.Error() {
	case CodeMalformedInput, CodeFactMissing, CodeMutationNotShipped, CodeOperationUnavailable, CodeInternalFailure:
		return err.Error()
	default:
		return CodeInternalFailure
	}
}
