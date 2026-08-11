package nativefallback

import (
	"context"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

type CoreReader interface {
	Identity(context.Context) coreinboundcontrol.CoreRuntimeIdentityV1
	Snapshot(context.Context, uint) (coreinboundcontrol.InboundFallbackSnapshotV1, error)
	PreviewFallbackPatch(context.Context, coreinboundcontrol.PreviewFallbackPatchRequestV1) (coreinboundcontrol.FallbackPatchPreviewV1, error)
}

type CoreControl interface {
	CoreReader
	PrepareCheckpoint(context.Context, coreinboundcontrol.PrepareCheckpointRequestV1) (coreinboundcontrol.CheckpointPreparationV1, error)
	InspectCheckpoint(context.Context, coreinboundcontrol.InspectCheckpointRequestV1) (coreinboundcontrol.CheckpointStatusV1, error)
	FindCheckpoint(context.Context, coreinboundcontrol.FindCheckpointRequestV1) (coreinboundcontrol.CheckpointStatusV1, error)
	ApplyFallbackPatch(context.Context, coreinboundcontrol.ApplyFallbackPatchRequestV1) (coreinboundcontrol.FallbackMutationResultV1, error)
	VerifyEffective(context.Context, coreinboundcontrol.VerifyEffectiveRequestV1) (coreinboundcontrol.EffectiveVerificationV1, error)
	RestoreCheckpoint(context.Context, coreinboundcontrol.RestoreCheckpointRequestV1) (coreinboundcontrol.RestoreCheckpointResultV1, error)
	ReleaseCheckpoint(context.Context, coreinboundcontrol.ReleaseCheckpointRequestV1) (coreinboundcontrol.CheckpointReleaseV1, error)
}

type ProviderDirectory interface {
	ProviderV2(string) (neutralfallback.ProviderV2, bool)
	ListReservationsV2(context.Context, neutralfallback.ListReservationsQueryV1) (neutralfallback.RegistryReservationsV2, error)
}

type NativeJournal interface {
	CreateNativeFallbackOperation(context.Context, repository.NativeFallbackOperationModel) (repository.NativeFallbackOperationModel, error)
	NativeFallbackOperation(context.Context, string) (repository.NativeFallbackOperationModel, error)
	ListNativeFallbackOperations(context.Context, []string) ([]repository.NativeFallbackOperationModel, error)
	ReservationMirror(context.Context, string) (repository.FallbackTargetLeaseModel, error)
	AdvanceNativeFallbackOperation(context.Context, repository.NativeFallbackJournalUpdate) (repository.NativeFallbackOperationModel, error)
	NativeFallbackState(context.Context, string) (domain.NativeFallbackStateV1, error)
	OperationByID(context.Context, string) (repository.OperationLockModel, error)
	OperationByHelperRevisionPrefix(context.Context, string) (repository.OperationLockModel, error)
	ArtifactByOperation(context.Context, string) (repository.ArtifactModel, error)
}

type ArtifactWriter interface {
	WriteRevision(context.Context, string, string, map[string][]byte) (repository.ArtifactModel, error)
}

type MutationMarker interface {
	MarkMutation(string, string) error
	VerifyRevision(string, string) (protectionartifacts.Manifest, error)
}

// TargetReader returns only the current target selected by the exact supplied
// reference identity. It never inventories alternatives for the planner.
type TargetReader interface {
	ResolveV2(context.Context, neutralfallback.FallbackTargetReferenceV2) (neutralfallback.FallbackTargetV2, error)
}

type ManagementEndpointFactsV1 struct {
	EndpointID             string
	EndpointRevision       string
	Network                string
	AddressFamily          string
	Address                string
	Port                   uint16
	Local                  bool
	ManagementReachability string
}

type ManagementIsolationResultV1 struct {
	State       string
	Revision    string
	ExpiresAt   time.Time
	ReasonCodes []string
}

type ManagementReader interface {
	ResolveIsolation(context.Context, string, ManagementEndpointFactsV1) (ManagementIsolationResultV1, error)
}

type PlanRequestV1 struct {
	InboundDatabaseID             uint
	ExpectedResourceID            string
	ExpectedSourceRevision        string
	ExpectedResourceRevision      string
	ExpectedConfigurationRevision string
	ExpectedEffectiveRevision     string
	TargetReference               neutralfallback.FallbackTargetReferenceV2
	ApplyGate                     string
}
