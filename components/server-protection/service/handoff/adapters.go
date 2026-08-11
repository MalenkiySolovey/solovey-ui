package handoff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

// ReviewedInboundWorkflow is the core-owned draft/apply boundary. A production
// implementation must use the normal reviewed inbound save workflow; it must
// not expose a DB handle or accept arbitrary config mutation from this package.
type ReviewedInboundWorkflow interface {
	PrepareReviewed(context.Context, OwnerSnapshot, Fence) error
	AbortReviewed(context.Context, OwnerSnapshot, Fence) error
	ApplyReviewed(context.Context, OwnerSnapshot, Fence) (CoreMutationResult, error)
	RestorePrevious(context.Context, OwnerSnapshot, OwnerSnapshot, Fence) (CoreMutationResult, error)
}

// FallbackWorkflow is implemented through a generic owner contribution. It
// keeps server-protection from importing a sibling component.
type FallbackWorkflow interface {
	PrepareLocalTarget(context.Context, OwnerSnapshot, Fence) error
	AbortLocalTarget(context.Context, OwnerSnapshot, Fence) error
	// ReleasePublicListener must compare-and-swap the resource/config revisions
	// in previous while it releases the source. A stale snapshot must fail
	// without changing the listener.
	ReleasePublicListener(context.Context, OwnerSnapshot, Fence) error
	RestorePublicListener(context.Context, OwnerSnapshot, Fence) error
}

// ProcessExecutor is intentionally one-method and sing-box-specific. It cannot
// express systemctl, service names, commands, flags, or shell text.
type ProcessExecutor interface {
	RestartSingBox(context.Context, Fence) error
}

type SerializedCoreRestart interface {
	RestartCore() error
}

// CoreProcessExecutor is the production bridge to ConfigService.RestartCore,
// which already serializes sing-box lifecycle changes. It has no service name
// or command-string escape hatch.
type CoreProcessExecutor struct{ Core SerializedCoreRestart }

func (e CoreProcessExecutor) RestartSingBox(ctx context.Context, _ Fence) error {
	if e.Core == nil {
		return ErrServiceDisabled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return e.Core.RestartCore()
}

type InboundWorkflowAdapter struct{ Workflow ReviewedInboundWorkflow }

func (a InboundWorkflowAdapter) Prepare(ctx context.Context, next OwnerSnapshot, fence Fence) error {
	if a.Workflow == nil {
		return ErrServiceDisabled
	}
	return a.Workflow.PrepareReviewed(ctx, next, fence)
}
func (a InboundWorkflowAdapter) AbortPrepare(ctx context.Context, next OwnerSnapshot, fence Fence) error {
	if a.Workflow == nil {
		return ErrServiceDisabled
	}
	return a.Workflow.AbortReviewed(ctx, next, fence)
}
func (a InboundWorkflowAdapter) Apply(ctx context.Context, next OwnerSnapshot, fence Fence) (CoreMutationResult, error) {
	if a.Workflow == nil {
		return CoreMutationResult{}, ErrServiceDisabled
	}
	return a.Workflow.ApplyReviewed(ctx, next, fence)
}
func (a InboundWorkflowAdapter) Rollback(ctx context.Context, previous, next OwnerSnapshot, fence Fence) (CoreMutationResult, error) {
	if a.Workflow == nil {
		return CoreMutationResult{}, ErrServiceDisabled
	}
	return a.Workflow.RestorePrevious(ctx, previous, next, fence)
}

type FallbackWorkflowAdapter struct{ Workflow FallbackWorkflow }

func (a FallbackWorkflowAdapter) Prepare(ctx context.Context, previous OwnerSnapshot, fence Fence) error {
	if a.Workflow == nil {
		return ErrServiceDisabled
	}
	return a.Workflow.PrepareLocalTarget(ctx, previous, fence)
}
func (a FallbackWorkflowAdapter) AbortPrepare(ctx context.Context, previous OwnerSnapshot, fence Fence) error {
	if a.Workflow == nil {
		return ErrServiceDisabled
	}
	return a.Workflow.AbortLocalTarget(ctx, previous, fence)
}
func (a FallbackWorkflowAdapter) Apply(ctx context.Context, previous OwnerSnapshot, fence Fence) error {
	if a.Workflow == nil {
		return ErrServiceDisabled
	}
	return a.Workflow.ReleasePublicListener(ctx, previous, fence)
}
func (a FallbackWorkflowAdapter) Rollback(ctx context.Context, previous OwnerSnapshot, fence Fence) error {
	if a.Workflow == nil {
		return ErrServiceDisabled
	}
	return a.Workflow.RestorePublicListener(ctx, previous, fence)
}

type SerializedRestartAdapter struct{ Executor ProcessExecutor }

func (a SerializedRestartAdapter) Restart(ctx context.Context, fence Fence) error {
	if a.Executor == nil {
		return ErrServiceDisabled
	}
	return a.Executor.RestartSingBox(ctx, fence)
}

// RegistryOwnershipAdapter reuses the generic host resource registry for
// owner revision/fingerprint and collision rechecks. It does not import any
// sibling component.
type RegistryOwnershipAdapter struct {
	Refresh func(context.Context) hostresources.ResourceSnapshot
}

func (a RegistryOwnershipAdapter) snapshot(ctx context.Context) hostresources.ResourceSnapshot {
	if a.Refresh != nil {
		return a.Refresh(ctx)
	}
	return hostresources.Refresh(ctx)
}

func (a RegistryOwnershipAdapter) Current(ctx context.Context, protocol, listen string, port int) (OwnerSnapshot, error) {
	for _, resource := range a.snapshot(ctx).Resources {
		owner := ownerSnapshotFromResource(resource)
		if owner.Protocol == strings.ToLower(protocol) && owner.Listen == listen && owner.Port == port {
			return owner, nil
		}
	}
	return OwnerSnapshot{}, ErrOwnerDisappeared
}

func (a RegistryOwnershipAdapter) Manifest(ctx context.Context) ([]OwnerSnapshot, error) {
	snapshot := a.snapshot(ctx)
	if len(snapshot.Errors) > 0 {
		return nil, errors.New("resource inventory is incomplete")
	}
	result := make([]OwnerSnapshot, 0, len(snapshot.Resources))
	for _, resource := range snapshot.Resources {
		result = append(result, ownerSnapshotFromResource(resource))
	}
	return result, nil
}

func ownerSnapshotFromResource(resource hostresources.ProtectableResource) OwnerSnapshot {
	protocol := strings.ToLower(strings.TrimSpace(resource.Protocol))
	if protocol == "http" || protocol == "https" || protocol == "stream" {
		protocol = "tcp"
	}
	return OwnerSnapshot{
		ResourceID: resource.ID, Owner: resource.Owner, Kind: resource.Kind,
		Protocol: protocol, Listen: resource.Listen, Port: resource.Port,
		ResourceRevision: resource.Capabilities.OwnerRevision,
		ConfigRevision:   resource.Capabilities.ConfigRevision,
		Fingerprint:      resource.Fingerprint,
		ProxyProtocol:    resource.Capabilities.AcceptsProxyProtocol == hostresources.CapabilityYes,
	}
}

type mutationMarker interface {
	MarkMutation(string, string) error
	HasMutationMarker(string) bool
}

// ArtifactCheckpoint publishes the immutable previous snapshot and manifest
// before the source listener can be released. MarkMutation is a separate,
// explicit boundary immediately before the first live change.
type ArtifactCheckpoint struct {
	Artifacts protectionartifacts.Service
	Marker    mutationMarker
}

type artifactMetadataReader interface {
	ArtifactByOperation(context.Context, string) (protectionrepository.ArtifactModel, error)
}

func (a ArtifactCheckpoint) Checkpoint(ctx context.Context, previous, next OwnerSnapshot, fence Fence) error {
	if a.Artifacts.Storage == nil || a.Artifacts.Store == nil || a.Marker == nil {
		return ErrServiceDisabled
	}
	before, err := json.Marshal(previous)
	if err != nil {
		return err
	}
	after, err := json.Marshal(next)
	if err != nil {
		return err
	}
	files := map[string][]byte{"resource-before.json": before, "resource-after.json": after}
	revision := checkpointRevision(fence.OperationID, before, after)
	_, err = a.Artifacts.WriteRevision(ctx, fence.OperationID, revision, files)
	if !errors.Is(err, protectionartifacts.ErrRevisionExists) {
		return err
	}
	reader, ok := a.Artifacts.Store.(artifactMetadataReader)
	if !ok {
		return err
	}
	artifact, loadErr := reader.ArtifactByOperation(ctx, fence.OperationID)
	if loadErr != nil || artifact.Revision != revision {
		return errors.Join(err, loadErr)
	}
	manifest, verifyErr := a.Artifacts.Storage.VerifyRevision(artifact.Revision, artifact.ManifestSHA256)
	if verifyErr != nil || manifest.OperationID != fence.OperationID || !manifestMatchesSnapshot(manifest, before, after) {
		return errors.Join(err, verifyErr)
	}
	return nil
}

func (a ArtifactCheckpoint) MarkMutation(_ context.Context, fence Fence) error {
	if a.Marker == nil {
		return ErrServiceDisabled
	}
	return a.Marker.MarkMutation(fence.OperationID, checkpointRevision(fence.OperationID, nil, nil))
}
func (a ArtifactCheckpoint) HasMutation(_ context.Context, fence Fence) (bool, error) {
	if a.Marker == nil {
		return false, ErrServiceDisabled
	}
	return a.Marker.HasMutationMarker(fence.OperationID), nil
}

func checkpointRevision(operationID string, _, _ []byte) string { return operationID }

func manifestMatchesSnapshot(manifest protectionartifacts.Manifest, before, after []byte) bool {
	expected := map[string][]byte{"resource-before.json": before, "resource-after.json": after}
	if len(manifest.Files) != len(expected) {
		return false
	}
	for _, file := range manifest.Files {
		data, ok := expected[file.Path]
		if !ok || file.Bytes != int64(len(data)) {
			return false
		}
		sum := sha256.Sum256(data)
		if file.SHA256 != hex.EncodeToString(sum[:]) {
			return false
		}
	}
	return true
}

type AdapterAvailability struct {
	InboundDraft   bool
	SingBoxRestart bool
	FallbackTarget bool
	Health         bool
	ProxyKnown     bool
	ProxyProtocol  bool
}

// HelperBackedAdapter negotiates the release-matched restricted helper and
// probes only the exact socket fenced in the operation lock.
type HelperBackedAdapter struct {
	Client       *protectionhelper.Client
	Availability AdapterAvailability
}

func (a HelperBackedAdapter) Capabilities(ctx context.Context) (Capabilities, error) {
	if a.Client == nil {
		return Capabilities{}, ErrServiceDisabled
	}
	response, err := a.Client.Execute(ctx, protectionhelper.Request{
		ProtocolVersion: protectionhelper.ProtocolVersion,
		Correlation:     protectionhelper.Correlation{OperationID: "capabilities", InstanceID: "port-handoff"},
		Operation:       protectionhelper.OperationCapabilities, Capabilities: &protectionhelper.CapabilitiesRequest{},
	})
	if err != nil {
		return Capabilities{}, err
	}
	if !response.OK || response.Capabilities == nil {
		return Capabilities{}, ErrServiceDisabled
	}
	listener := protectionhelper.CapabilityAvailable(response.Capabilities, protectionhelper.OperationListenerProbe)
	return Capabilities{
		Revision: response.Capabilities.Revision, ProxyProtocol: a.Availability.ProxyKnown && a.Availability.ProxyProtocol,
		InboundDraft: a.Availability.InboundDraft, SingBoxRestart: a.Availability.SingBoxRestart,
		ListenerOwnership: listener, FallbackTarget: a.Availability.FallbackTarget, Health: a.Availability.Health, ExactListener: listener,
	}, nil
}

func (a HelperBackedAdapter) Verify(ctx context.Context, owner OwnerSnapshot, fence Fence) error {
	if a.Client == nil {
		return ErrServiceDisabled
	}
	address, err := netip.ParseAddr(strings.Trim(owner.Listen, "[]"))
	if err != nil || address.IsUnspecified() || address.IsMulticast() {
		return fmt.Errorf("%w: exact IP required", ErrListenerVerify)
	}
	expectedOwner, err := helperListenerOwner(owner)
	if err != nil {
		return err
	}
	response, err := a.Client.Execute(ctx, protectionhelper.Request{
		ProtocolVersion: protectionhelper.ProtocolVersion,
		Correlation:     protectionhelper.Correlation{OperationID: fence.OperationID, InstanceID: fence.InstanceID, LockRevision: fence.Revision},
		Operation:       protectionhelper.OperationListenerProbe,
		ListenerProbe:   &protectionhelper.ListenerProbeRequest{Purpose: protectionhelper.ProbePortHandoff, Network: strings.ToLower(owner.Protocol), Address: address.Unmap().String(), Port: owner.Port, ExpectedOwner: expectedOwner, ExpectedPID: fence.PID},
	})
	if err != nil {
		return err
	}
	if !response.OK || response.ListenerProbe == nil || !response.ListenerProbe.Reachable || !response.ListenerProbe.OwnerMatched || response.ListenerProbe.OwnerClass != expectedOwner {
		return ErrListenerVerify
	}
	return nil
}

func helperListenerOwner(owner OwnerSnapshot) (protectionhelper.ListenerOwner, error) {
	switch strings.ToLower(strings.TrimSpace(owner.Kind)) {
	case "inbound":
		// sing-box is embedded in the panel process in the current core runtime.
		return protectionhelper.ListenerOwnerPanel, nil
	case "public_site", "fallback", "fallback_site", "panel_web", "subscription":
		return protectionhelper.ListenerOwnerPanel, nil
	default:
		if strings.EqualFold(strings.TrimSpace(owner.Owner), "sing-box") {
			return protectionhelper.ListenerOwnerSingBox, nil
		}
		return "", fmt.Errorf("%w: listener owner class is unknown", ErrListenerVerify)
	}
}

type HealthExecutor interface {
	Check(context.Context, []HealthTarget) ([]HealthResult, error)
}

// OwnerHealthExecutor uses only owner-published in-process health contracts.
// It never calls a subscription URL or public path.
type OwnerHealthExecutor struct{}

func (OwnerHealthExecutor) Check(ctx context.Context, targets []HealthTarget) ([]HealthResult, error) {
	results := make([]HealthResult, 0, len(targets))
	for _, target := range targets {
		result := componenthealth.Check(ctx, target.ResourceID)
		results = append(results, HealthResult{Target: target, OK: result.Status == componenthealth.StatusOK, Fact: result.FactCode})
	}
	return results, nil
}

type BoundedHealthAdapter struct {
	Executor HealthExecutor
	Timeout  time.Duration
}

func (a BoundedHealthAdapter) Check(ctx context.Context, targets []HealthTarget) ([]HealthResult, error) {
	if a.Executor == nil {
		return nil, ErrServiceDisabled
	}
	timeout := a.Timeout
	if timeout <= 0 || timeout > 15*time.Second {
		timeout = 15 * time.Second
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return a.Executor.Check(bounded, append([]HealthTarget(nil), targets...))
}

// ArtifactRecoveryUX creates only the existing redacted recovery bundle. Raw
// rollback snapshots stay in the protected revision directory.
type ArtifactRecoveryUX struct {
	Storage    *protectionartifacts.Storage
	Repository *protectionrepository.Repository
}

func (a ArtifactRecoveryUX) Record(ctx context.Context, facts RecoveryFacts) error {
	if a.Storage == nil || a.Repository == nil {
		return ErrServiceDisabled
	}
	artifact, err := a.Repository.ArtifactByOperation(ctx, facts.OperationID)
	if err != nil {
		return err
	}
	bundle, err := a.Storage.CreateRecoveryBundle(protectionartifacts.RecoveryInput{
		OperationID: facts.OperationID, Revision: artifact.Revision, State: facts.State,
		ResourceID: facts.ResourceID, ResourceKind: "port_owner", Protocol: facts.Protocol,
		Listen: facts.Listen, Port: facts.Port, FromOwner: facts.FromOwner, ToOwner: facts.ToOwner,
		UpdatedAt: time.Now().Unix(),
	}, artifact.ManifestSHA256)
	if err != nil {
		return err
	}
	artifact.Scope = "recovery"
	artifact.Bytes += bundle.Bytes
	artifact.UpdatedAt = time.Now().Unix()
	return a.Repository.SaveArtifact(ctx, &artifact)
}

var (
	_ InboundDraftAdapter      = InboundWorkflowAdapter{}
	_ FallbackTargetAdapter    = FallbackWorkflowAdapter{}
	_ SingBoxRestartAdapter    = SerializedRestartAdapter{}
	_ DurableSnapshotAdapter   = ArtifactCheckpoint{}
	_ HelperAdapter            = HelperBackedAdapter{}
	_ ExactListenerAdapter     = HelperBackedAdapter{}
	_ HealthAdapter            = BoundedHealthAdapter{}
	_ RecoveryUXAdapter        = ArtifactRecoveryUX{}
	_ ListenerOwnershipAdapter = RegistryOwnershipAdapter{}
)
