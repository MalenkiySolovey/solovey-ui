package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

var safeFact = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type RecoveryInput struct {
	OperationID               string
	Revision                  string
	DesiredRevision           string
	CandidateSHA256           string
	PreviousRevision          string
	PreviousSHA256            string
	RollbackSHA256            string
	PreviousTablePresent      bool
	ArtifactManifestSHA256    string
	State                     string
	ResourceID                string
	ResourceKind              string
	Protocol                  string
	Listen                    string
	Port                      int
	FromOwner                 string
	ToOwner                   string
	CreatedAt                 int64
	UpdatedAt                 int64
	Health                    []HealthCheck
	Strategy                  string
	PlanDigest                string
	SocketClaimRevision       string
	BackendReferenceRevision  string
	BackendReferenceRevisions []string
	SelectorSetRevision       string
	MapRevision               string
	UpstreamIDSetRevision     string
	TargetAuthorities         []TargetAuthorityRecoveryInput
	ProviderLeaseID           string
	ProviderLeaseRevision     string
	ProviderLeaseState        string
	ExpectedActiveRevision    string
	ActualActiveRevision      string
	ProcessRevision           string
	ListenerRevision          string
	FailedStage               string
	RollbackAttemptCount      int
	PermittedNextAction       string
}

type TargetAuthorityRecoveryInput struct {
	Kind     string
	ID       string
	Revision string
	State    string
}

type TargetAuthorityRecoveryFact struct {
	Kind         string `json:"kind"`
	AuthorityRef string `json:"authorityRef"`
	RevisionRef  string `json:"revisionRef"`
	State        string `json:"state"`
}

type HealthCheck struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	FactCode string `json:"factCode"`
}

type RecoverySummary struct {
	OperationID              string                        `json:"operationId"`
	ArtifactRevision         string                        `json:"artifactRevision,omitempty"`
	DesiredRevision          string                        `json:"desiredRevision,omitempty"`
	CandidateSHA256          string                        `json:"candidateSha256,omitempty"`
	PreviousRevision         string                        `json:"previousRevision,omitempty"`
	PreviousSHA256           string                        `json:"previousSha256,omitempty"`
	RollbackSHA256           string                        `json:"rollbackSha256,omitempty"`
	PreviousTablePresent     bool                          `json:"previousTablePresent"`
	ArtifactManifestSHA256   string                        `json:"artifactManifestSha256,omitempty"`
	State                    string                        `json:"state"`
	ResourceRef              string                        `json:"resourceRef,omitempty"`
	ResourceKind             string                        `json:"resourceKind,omitempty"`
	Protocol                 string                        `json:"protocol,omitempty"`
	Listen                   string                        `json:"listen,omitempty"`
	Port                     int                           `json:"port,omitempty"`
	FromOwner                string                        `json:"fromOwner,omitempty"`
	ToOwner                  string                        `json:"toOwner,omitempty"`
	CreatedAt                int64                         `json:"createdAt"`
	UpdatedAt                int64                         `json:"updatedAt"`
	RecoveryFacts            []string                      `json:"recoveryFacts"`
	Strategy                 string                        `json:"strategy,omitempty"`
	PlanDigest               string                        `json:"planDigest,omitempty"`
	SocketClaimRevision      string                        `json:"socketClaimRevision,omitempty"`
	BackendReferenceRevision string                        `json:"backendReferenceRevision,omitempty"`
	TargetReferenceRevisions []string                      `json:"targetReferenceRevisions,omitempty"`
	SelectorSetRevision      string                        `json:"selectorSetRevision,omitempty"`
	MapRevision              string                        `json:"mapRevision,omitempty"`
	UpstreamIDSetRevision    string                        `json:"upstreamIdSetRevision,omitempty"`
	TargetAuthorities        []TargetAuthorityRecoveryFact `json:"targetAuthorities,omitempty"`
	ProviderLeaseRef         string                        `json:"providerLeaseRef,omitempty"`
	ProviderLeaseRevision    string                        `json:"providerLeaseRevision,omitempty"`
	ProviderLeaseState       string                        `json:"providerLeaseState,omitempty"`
	ExpectedActiveRevision   string                        `json:"expectedActiveRevision,omitempty"`
	ActualActiveRevision     string                        `json:"actualActiveRevision,omitempty"`
	ProcessRevision          string                        `json:"processRevision,omitempty"`
	ListenerRevision         string                        `json:"listenerRevision,omitempty"`
	FailedStage              string                        `json:"failedStage,omitempty"`
	RollbackAttemptCount     int                           `json:"rollbackAttemptCount,omitempty"`
	PermittedNextAction      string                        `json:"permittedNextAction,omitempty"`
}

type RecoveryBundle struct {
	RelativePath string
	Bytes        int64
}

// CreateRecoveryBundle emits only typed, bounded facts. It never copies the
// rollback payloads themselves into the manual bundle.
func (s *Storage) CreateRecoveryBundle(input RecoveryInput, expectedManifestSHA string) (RecoveryBundle, error) {
	if err := validateOperationID(input.OperationID); err != nil {
		return RecoveryBundle{}, err
	}
	if err := validateSegment(input.Revision); err != nil {
		return RecoveryBundle{}, err
	}
	manifest, err := s.VerifyRevision(input.Revision, expectedManifestSHA)
	if err != nil {
		return RecoveryBundle{}, err
	}
	if manifest.OperationID != input.OperationID {
		return RecoveryBundle{}, errors.New("artifact manifest operation mismatch")
	}
	summary := RecoverySummary{
		OperationID: input.OperationID, State: safeCode(input.State), ResourceRef: opaqueRef(input.ResourceID),
		ResourceKind: safeCode(input.ResourceKind), Protocol: safeCode(input.Protocol), Listen: safeListen(input.Listen), Port: safePort(input.Port),
		FromOwner: safeOwner(input.FromOwner), ToOwner: safeOwner(input.ToOwner), CreatedAt: input.CreatedAt, UpdatedAt: input.UpdatedAt,
		RecoveryFacts: recoveryFacts(input.State),
	}
	if input.ResourceKind == "fronting" {
		populateFrontingRecovery(&summary, input)
	} else if input.ResourceKind == "firewall" {
		populateFirewallRecovery(&summary, input)
	}
	health := make([]HealthCheck, 0, len(input.Health))
	for _, check := range input.Health {
		health = append(health, HealthCheck{ID: safeCode(check.ID), Status: safeCode(check.Status), FactCode: safeCode(check.FactCode)})
	}
	type recoveryManifestFile struct {
		Artifact string `json:"artifact"`
		SHA256   string `json:"sha256"`
		Bytes    int64  `json:"bytes"`
	}
	publicFiles := make([]recoveryManifestFile, 0, len(manifest.Files))
	for index, file := range manifest.Files {
		publicFiles = append(publicFiles, recoveryManifestFile{Artifact: fmt.Sprintf("artifact-%03d", index+1), SHA256: file.SHA256, Bytes: file.Bytes})
	}
	publicManifest := struct {
		Version                int                    `json:"version"`
		OperationID            string                 `json:"operationId"`
		RevisionRef            string                 `json:"revisionRef"`
		ArtifactRevision       string                 `json:"artifactRevision,omitempty"`
		DesiredRevision        string                 `json:"desiredRevision,omitempty"`
		CandidateSHA256        string                 `json:"candidateSha256,omitempty"`
		PreviousRevision       string                 `json:"previousRevision,omitempty"`
		PreviousSHA256         string                 `json:"previousSha256,omitempty"`
		RollbackSHA256         string                 `json:"rollbackSha256,omitempty"`
		PreviousTablePresent   bool                   `json:"previousTablePresent"`
		ArtifactManifestSHA256 string                 `json:"artifactManifestSha256,omitempty"`
		CreatedAt              int64                  `json:"createdAt"`
		Files                  []recoveryManifestFile `json:"files"`
	}{Version: manifest.Version, OperationID: manifest.OperationID, RevisionRef: opaqueRef(manifest.Revision), CreatedAt: manifest.CreatedAt, Files: publicFiles}
	if input.ResourceKind == "fronting" {
		publicManifest.ArtifactRevision = safeExactReference(manifest.Revision)
		publicManifest.DesiredRevision = safeRevisionReference(input.DesiredRevision)
		publicManifest.CandidateSHA256 = safeSHA256(input.CandidateSHA256)
		publicManifest.PreviousRevision = safeRevisionReference(input.PreviousRevision)
		publicManifest.PreviousSHA256 = safeSHA256(input.PreviousSHA256)
		publicManifest.ArtifactManifestSHA256 = safeSHA256(input.ArtifactManifestSHA256)
	} else if input.ResourceKind == "firewall" {
		publicManifest.ArtifactRevision = safeExactReference(manifest.Revision)
		publicManifest.DesiredRevision = safeRevisionReference(input.DesiredRevision)
		publicManifest.CandidateSHA256 = safeSHA256(input.CandidateSHA256)
		publicManifest.PreviousRevision = safeRevisionReference(input.PreviousRevision)
		publicManifest.PreviousSHA256 = safeSHA256(input.PreviousSHA256)
		publicManifest.RollbackSHA256 = safeSHA256(input.RollbackSHA256)
		publicManifest.PreviousTablePresent = input.PreviousTablePresent
		publicManifest.ArtifactManifestSHA256 = safeSHA256(input.ArtifactManifestSHA256)
	}
	values := map[string]any{
		"summary.json": summary,
		"health.json": struct {
			Checks []HealthCheck `json:"checks"`
		}{health},
		"artifacts-manifest.json": publicManifest,
		"recovery-actions.json": struct {
			Actions []recoveryAction `json:"actions"`
		}{safeRecoveryActions(input.ResourceKind)},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	relativeDir := filepathSlash("recovery", input.OperationID)
	if _, err := s.ensureDir(relativeDir); err != nil {
		return RecoveryBundle{}, err
	}
	var bytes int64
	for _, name := range []string{"summary.json", "health.json", "artifacts-manifest.json", "recovery-actions.json"} {
		data, marshalErr := json.MarshalIndent(values[name], "", "  ")
		if marshalErr != nil {
			return RecoveryBundle{}, marshalErr
		}
		data = append(data, '\n')
		if err := s.atomicWrite(filepathSlash(relativeDir, name), data); err != nil {
			return RecoveryBundle{}, err
		}
		bytes += int64(len(data))
	}
	return RecoveryBundle{RelativePath: relativeDir, Bytes: bytes}, nil
}

type recoveryAction struct {
	Program string   `json:"program"`
	Args    []string `json:"args"`
	Purpose string   `json:"purpose"`
}

func safeFirewallRecoveryActions() []recoveryAction {
	return []recoveryAction{
		{Program: "solovey-protect-helper", Args: []string{"--smoke"}, Purpose: "verify_release_matched_helper"},
		{Program: "nft", Args: []string{"list", "table", "inet", "solovey_protection"}, Purpose: "inspect_managed_table_only"},
	}
}

func safeRecoveryActions(kind string) []recoveryAction {
	if kind == "fronting" {
		return []recoveryAction{
			{Program: "solovey-protect-helper", Args: []string{"--smoke"}, Purpose: "verify_release_matched_helper"},
			{Program: "operator_review", Args: []string{"fronting_operation", "artifact_hashes", "active_revision"}, Purpose: "review_before_bounded_manual_repair"},
		}
	}
	return safeFirewallRecoveryActions()
}

func validRevisionReference(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func safeRevisionReference(value string) string {
	if validRevisionReference(value) {
		return value
	}
	return ""
}

func safeSHA256(value string) string {
	if len(value) != 64 {
		return ""
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return ""
		}
	}
	return value
}

func safeExactReference(value string) string {
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, character := range value {
		if !(character == '-' || character == '_' || character == '.' || character == ':' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9') {
			return ""
		}
	}
	return value
}

func populateFrontingRecovery(summary *RecoverySummary, input RecoveryInput) {
	summary.ArtifactRevision = safeExactReference(input.Revision)
	summary.DesiredRevision = safeRevisionReference(input.DesiredRevision)
	summary.CandidateSHA256 = safeSHA256(input.CandidateSHA256)
	summary.PreviousRevision = safeRevisionReference(input.PreviousRevision)
	summary.PreviousSHA256 = safeSHA256(input.PreviousSHA256)
	summary.ArtifactManifestSHA256 = safeSHA256(input.ArtifactManifestSHA256)
	summary.Strategy = safeCode(input.Strategy)
	summary.PlanDigest = safeSHA256(input.PlanDigest)
	summary.SocketClaimRevision = safeSHA256(input.SocketClaimRevision)
	summary.BackendReferenceRevision = safeSHA256(input.BackendReferenceRevision)
	summary.TargetReferenceRevisions = safeBoundedRevisionList(input.BackendReferenceRevisions, 64)
	summary.SelectorSetRevision = safeSHA256(input.SelectorSetRevision)
	summary.MapRevision = safeSHA256(input.MapRevision)
	summary.UpstreamIDSetRevision = safeSHA256(input.UpstreamIDSetRevision)
	for _, authority := range input.TargetAuthorities {
		if len(summary.TargetAuthorities) >= 64 {
			break
		}
		kind, state := safeCode(authority.Kind), safeCode(authority.State)
		if kind == "" || state == "" || authority.ID == "" || authority.Revision == "" {
			continue
		}
		summary.TargetAuthorities = append(summary.TargetAuthorities, TargetAuthorityRecoveryFact{
			Kind: kind, AuthorityRef: opaqueRef(authority.ID), RevisionRef: opaqueRef(authority.Revision), State: state,
		})
	}
	summary.ProviderLeaseRef = opaqueRef(input.ProviderLeaseID)
	summary.ProviderLeaseRevision = safeSHA256(input.ProviderLeaseRevision)
	summary.ProviderLeaseState = safeCode(input.ProviderLeaseState)
	summary.ExpectedActiveRevision = safeRevisionReference(input.ExpectedActiveRevision)
	summary.ActualActiveRevision = safeRevisionReference(input.ActualActiveRevision)
	summary.ProcessRevision = safeSHA256(input.ProcessRevision)
	summary.ListenerRevision = safeSHA256(input.ListenerRevision)
	summary.FailedStage = safeCode(input.FailedStage)
	if input.RollbackAttemptCount > 0 && input.RollbackAttemptCount < 3 {
		summary.RollbackAttemptCount = input.RollbackAttemptCount
	}
	summary.PermittedNextAction = safeCode(input.PermittedNextAction)
}

func safeBoundedRevisionList(values []string, limit int) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = safeSHA256(value)
		if value == "" || seen[value] || len(result) >= limit {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func populateFirewallRecovery(summary *RecoverySummary, input RecoveryInput) {
	summary.ArtifactRevision = safeExactReference(input.Revision)
	summary.DesiredRevision = safeRevisionReference(input.DesiredRevision)
	summary.CandidateSHA256 = safeSHA256(input.CandidateSHA256)
	summary.PreviousRevision = safeRevisionReference(input.PreviousRevision)
	summary.PreviousSHA256 = safeSHA256(input.PreviousSHA256)
	summary.RollbackSHA256 = safeSHA256(input.RollbackSHA256)
	summary.PreviousTablePresent = input.PreviousTablePresent
	summary.ArtifactManifestSHA256 = safeSHA256(input.ArtifactManifestSHA256)
}

// CreateEmergencyRecoveryBundle is used only when the rollback manifest cannot
// be trusted. It records the checksum failure without copying unverified names
// or payloads into the manual bundle.
func (s *Storage) CreateEmergencyRecoveryBundle(input RecoveryInput, status string) (RecoveryBundle, error) {
	if err := validateOperationID(input.OperationID); err != nil {
		return RecoveryBundle{}, err
	}
	if err := validateSegment(input.Revision); err != nil {
		return RecoveryBundle{}, err
	}
	summary := RecoverySummary{
		OperationID: input.OperationID, State: safeCode(input.State), ResourceRef: opaqueRef(input.ResourceID),
		ResourceKind: safeCode(input.ResourceKind), Protocol: safeCode(input.Protocol), Listen: safeListen(input.Listen), Port: safePort(input.Port),
		FromOwner: safeOwner(input.FromOwner), ToOwner: safeOwner(input.ToOwner), CreatedAt: input.CreatedAt, UpdatedAt: input.UpdatedAt,
		RecoveryFacts: append(recoveryFacts(input.State), "artifact_integrity_failed"),
	}
	if input.ResourceKind == "fronting" {
		populateFrontingRecovery(&summary, input)
	} else if input.ResourceKind == "firewall" {
		populateFirewallRecovery(&summary, input)
	}
	health := struct {
		Checks []HealthCheck `json:"checks"`
	}{Checks: []HealthCheck{{ID: "artifact_integrity", Status: "failed", FactCode: safeCode(status)}}}
	manifest := struct {
		Version                int    `json:"version"`
		OperationID            string `json:"operationId"`
		RevisionRef            string `json:"revisionRef"`
		ArtifactRevision       string `json:"artifactRevision,omitempty"`
		DesiredRevision        string `json:"desiredRevision,omitempty"`
		CandidateSHA256        string `json:"candidateSha256,omitempty"`
		PreviousRevision       string `json:"previousRevision,omitempty"`
		PreviousSHA256         string `json:"previousSha256,omitempty"`
		RollbackSHA256         string `json:"rollbackSha256,omitempty"`
		PreviousTablePresent   bool   `json:"previousTablePresent"`
		ArtifactManifestSHA256 string `json:"artifactManifestSha256,omitempty"`
		Status                 string `json:"status"`
		Files                  []File `json:"files"`
	}{Version: ManifestVersion, OperationID: input.OperationID, RevisionRef: opaqueRef(input.Revision), Status: safeCode(status), Files: []File{}}
	if input.ResourceKind == "fronting" {
		manifest.ArtifactRevision = safeExactReference(input.Revision)
		manifest.DesiredRevision = safeRevisionReference(input.DesiredRevision)
		manifest.CandidateSHA256 = safeSHA256(input.CandidateSHA256)
		manifest.PreviousRevision = safeRevisionReference(input.PreviousRevision)
		manifest.PreviousSHA256 = safeSHA256(input.PreviousSHA256)
		manifest.ArtifactManifestSHA256 = safeSHA256(input.ArtifactManifestSHA256)
	} else if input.ResourceKind == "firewall" {
		manifest.ArtifactRevision = safeExactReference(input.Revision)
		manifest.DesiredRevision = safeRevisionReference(input.DesiredRevision)
		manifest.CandidateSHA256 = safeSHA256(input.CandidateSHA256)
		manifest.PreviousRevision = safeRevisionReference(input.PreviousRevision)
		manifest.PreviousSHA256 = safeSHA256(input.PreviousSHA256)
		manifest.RollbackSHA256 = safeSHA256(input.RollbackSHA256)
		manifest.PreviousTablePresent = input.PreviousTablePresent
		manifest.ArtifactManifestSHA256 = safeSHA256(input.ArtifactManifestSHA256)
	}
	values := []struct {
		name  string
		value any
	}{
		{"summary.json", summary},
		{"health.json", health},
		{"artifacts-manifest.json", manifest},
		{"recovery-actions.json", struct {
			Actions []recoveryAction `json:"actions"`
		}{safeRecoveryActions(input.ResourceKind)}},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	relativeDir := filepathSlash("recovery", input.OperationID)
	if _, err := s.ensureDir(relativeDir); err != nil {
		return RecoveryBundle{}, err
	}
	var bytes int64
	for _, value := range values {
		data, err := json.MarshalIndent(value.value, "", "  ")
		if err != nil {
			return RecoveryBundle{}, err
		}
		data = append(data, '\n')
		if err := s.atomicWrite(filepathSlash(relativeDir, value.name), data); err != nil {
			return RecoveryBundle{}, err
		}
		bytes += int64(len(data))
	}
	return RecoveryBundle{RelativePath: relativeDir, Bytes: bytes}, nil
}

func safeCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if safeFact.MatchString(value) {
		return value
	}
	return "unknown"
}

func safeOwner(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 || strings.ContainsAny(value, `/\\:?&#=%`) {
		return "redacted"
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r == '.' || r == ' ' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return "redacted"
		}
	}
	return value
}

func safeListen(value string) string {
	value = strings.TrimSpace(value)
	if value == "*" {
		return value
	}
	if address, err := netip.ParseAddr(strings.Trim(value, "[]")); err == nil {
		return address.String()
	}
	return "redacted"
}

func safePort(value int) int {
	if value >= 1 && value <= 65535 {
		return value
	}
	return 0
}

func opaqueRef(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:6])
}

func recoveryFacts(state string) []string {
	facts := []string{"preserve_bundle", "review_operation_audit", "verify_panel_access"}
	switch state {
	case "rollback_failed":
		return append(facts, "manual_repair_required", "do_not_forget_state_before_review")
	case "health_failed":
		return append(facts, "health_verification_required")
	case "rolling_back":
		return append(facts, "rollback_interrupted")
	default:
		return facts
	}
}

func filepathSlash(parts ...string) string {
	return strings.Join(parts, "/")
}

func (s RecoverySummary) ValidateSafe() error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	text := strings.ToLower(string(data))
	for _, forbidden := range []string{"private key", "cookie", "subscription_url", "admin_path", "shell", "command"} {
		if strings.Contains(text, forbidden) {
			return fmt.Errorf("unsafe recovery summary contains %s", forbidden)
		}
	}
	return nil
}
