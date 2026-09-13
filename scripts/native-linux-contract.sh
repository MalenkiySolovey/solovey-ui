#!/usr/bin/env bash

set -Eeuo pipefail

source_dir=

while [[ $# -gt 0 ]]; do
    case "$1" in
        --source-root)
            [[ $# -ge 2 ]] || { printf '[native-linux-contract] ERROR: --source-root requires a value\n' >&2; exit 2; }
            source_dir=$2
            shift 2
            ;;
        --help|-h)
            printf 'Usage: bash scripts/native-linux-contract.sh [--source-root <repository>]\n'
            exit 0
            ;;
        *)
            printf '[native-linux-contract] ERROR: unknown argument: %s\n' "$1" >&2
            exit 2
            ;;
    esac
done

if [[ -z "$source_dir" ]]; then
    source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
else
    source_dir=$(cd -- "$source_dir" && pwd -P)
fi

go_program=${SOLOVEY_GO_PROGRAM:-$(command -v go || true)}
[[ "$(uname -s)" == Linux ]] || { printf '[native-linux-contract] ERROR: a real Linux kernel is required\n' >&2; exit 1; }
[[ -d /proc/self/fd && -r /proc/self/stat && -r /proc/self/net/tcp ]] || {
    printf '[native-linux-contract] ERROR: required live procfs process/socket views are unavailable\n' >&2
    exit 1
}
[[ -x "$go_program" ]] || { printf '[native-linux-contract] ERROR: Go program is unavailable: %s\n' "$go_program" >&2; exit 1; }
"$go_program" version | grep -Eq '^go version go1\.26\.6 linux/' || {
    printf '[native-linux-contract] ERROR: exact Go 1.26.6 Linux toolchain is required\n' >&2
    exit 1
}

export GOENV=off
export GOTOOLCHAIN=local
export GOWORK=off
export GO111MODULE=on

run_test() {
    local layer=$1
    local pattern=$2
    local package=$3
    printf '[native-linux-contract] %s: %s\n' "$layer" "$package"
    "$go_program" test -count=1 -run "$pattern" "$package"
    printf '[native-linux-contract] %s: PASS\n' "$layer"
}

run_contract_test() {
    local layer=$1
    local pattern=$2
    local package=$3
    printf '[native-linux-contract] %s: %s\n' "$layer" "$package"
    "$go_program" test -count=1 -tags solovey_contract -run "$pattern" "$package"
    printf '[native-linux-contract] %s: PASS\n' "$layer"
}

cd -- "$source_dir"

run_test 'Layer A real listener evidence' '.' './internal/ops/listenerevidence'
run_test 'Layer B production broker diagnostic composition' \
    '^(TestListenerFailureStagesCrossProductionBrokerWithOneSanitizedPublicFailure|TestProductionBrokerDispatchPublishesTypedRecentDiagnosticAndKeepsPublicFailureSanitized|TestDefaultRecentDiagnosticReadRequiresRoot|TestRecentDiagnosticRingIsAlwaysReadableBoundedAndOwnerClassified|TestRecentDiagnosticRingIgnoresUntypedDenialsAndRejectsUnsafeFields|TestRecentDiagnosticRingRejectsUntrustedPathAndDocument|TestHandlerDiagnosticPreservesBoundedInternalBranchAndSanitizesPublicError)$' \
    './internal/ops/privilegedbroker'
run_contract_test 'Layer B real SSH handler to persisted operator-readable ring' \
    '^TestRealSSHObserveHandlerPersistsAndRereadsEveryListenerFailureFamily$' \
    './internal/ops/sshbroker'
run_test 'Layer C real process executable identity fence' \
    '^TestRealProcessFenceUsesRawExecutableContentIdentityAndObjectGeneration$' \
    './internal/ops/sshbroker'
run_test 'Layer B root-only diagnostic operator' \
    '^TestOperatorRecentIsRootPathFixedReadOnlyAndBounded$' \
    './cmd/solovey-evidence'
run_test 'Layer C OpenWrt/Dropbear physical shape' \
    '^(TestListenerAuthorityValidationDefinesItsOwnBoundedReason|TestDropbearUCISelectsNamedAndAnonymousSectionsWithoutFixedIndexAuthority|TestDropbearTargetAmbiguityAndMalformedEvidenceFailClosed|TestDropbearProcdProjectionCorrelatesExactSectionAndConfiguredPort|TestDropbearCommandBindsAddressQualifiedSocket|TestDropbearCommandRequiresCompleteRuntimeListenerSet|TestDropbearProjectionRejectsRealReusePortEndpointAmbiguity)$' \
    './internal/ops/sshbroker'
run_test 'Layer D post-observation registry validation' \
    '^TestRegistryValidatesListenerAuthorityAfterProviderObservation$' \
    './componenthost/hostsurface'
run_test 'Layer D coherent resource refresh' \
    '^TestSnapshotExcludingDoesNotInvokeOwnerOrPolluteSharedCache$' \
    './componenthost/resources'
run_test 'Layer D coherent SSH projection' \
    '^TestProviderConsumesOneAdjacentSSHGenerationAfterOtherResources$' \
    './components/server-protection/service/hostsurface'
run_test 'Layer D real listener to typed firewall candidate' \
    '^(TestNativeLinuxRealListenerAuthorityReachesTypedFirewallCandidate|TestStockDropbearAuthorityReachesPreparedTypedFirewallCandidate|TestCurrentSSHOwnerCrossesSecondBoundaryThroughHostSurfaceAndManagement|TestCurrentSSHProductionBaselineUsesOneOwnerGeneration|TestManagementPlanProductionHealthLifecycle)$' \
    './components/server-protection/service/firewall'
run_test 'Layer D current SSH owner read model' \
    '^(TestCurrentReadUsesOneObservationWithoutWorkflowHistory|TestSSHPreviewValidatesAfterObservationCompletes|TestCurrentPostureScopeRetainsOneGenerationAndRejectsExpiry|TestCurrentReadObservesSSHAfterSlowNeutralProviders|TestSSHPreviewRevisionBindsAuthorityInsteadOfObservationClock)$' \
    './service/sshmanagement'
run_test 'Layer D exact socket coverage classification' \
    '^TestExactSocketCoverageAvoidsOnlyProvenFalseDualStackAmbiguity$' \
    './components/server-protection/service/resources'
run_test 'Layer F current SSH HTTP composition' \
    '^TestSSHCurrentHTTPExposesOneCurrentOwnerWithoutWorkflowHistory$' \
    './api'
run_test 'Layer E recovery identity and transition' \
    '^(TestPanelRecoveryBindsDirectAndTrustedProxyIdentity|TestCredentialTransitionRequiresLaterRealAuthenticationToReissueRecovery|TestPanelRecoveryRejectsUnknownAndInvalidIdentityWithBoundedDisposition|TestPanelRecoveryBindingDriftRemovesPreviouslyValidEvidence)$' \
    './service/sshmanagement'
run_test 'Layer E typed authentication event' \
    '^TestPanelAuthenticationEventNotifierStaleCleanupAndTypedDelivery$' \
    './service'
run_test 'Layer E trusted-source domain contract' \
    '^Test(TrustedSourceNormalizationAndFamily|TrustedSourceRejectsUnusableAndAmbiguousAddresses|BroadTrustedSourceRequiresExactTypedConfirmation)$' \
    './components/server-protection/service/policy'
run_test 'Layer E trusted-source API contract' \
    '^Test(TrustedSourceCreateNormalizesRejectsDuplicatesAndExpiredInput|BroadTrustedSourceRequiresTypedConfirmation|TrustedSourceProposalPreservesClientIdentityAuthority)$' \
    './components/server-protection/api'
run_test 'Layer F canonical firewall preview arrays' \
    '^TestFirewallPreviewEmptyCollectionsMarshalAsArrays$' \
    './components/server-protection/service/firewall'
run_test 'Layer F API preview collections' \
    '^TestMVPAPIInventoryProfileAndMissingApplyCapability$' \
    './components/server-protection/api'
