package localproxy

import (
	"context"
	"errors"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

type Reconciler struct {
	Controller *Controller
}

func (r Reconciler) Reconcile(ctx context.Context, operation protectionrepository.OperationLockModel) (protectionoperations.ReconcileDecision, error) {
	if r.Controller == nil || r.Controller.ready() != nil {
		return protectionoperations.ReconcileDecision{}, errors.New("local proxy reconciler unavailable")
	}
	hostsurface.Default.Reconcile(ctx)
	state, err := r.Controller.Repository.LocalProxyStateByOperation(ctx, operation.OperationID)
	if err != nil {
		return r.reconcileWithoutMirror(ctx, operation)
	}
	plan, lease, err := r.Controller.resolveStoredPlanAndLease(ctx, state, false, true)
	if err != nil {
		state.ActualState, state.RecoveryRequired = string(StateRecoveryRequired), true
		if saveErr := r.Controller.Repository.SaveLocalProxyState(ctx, state); saveErr != nil {
			return protectionoperations.ReconcileDecision{}, saveErr
		}
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "local_proxy_authority_unproven"}, nil
	}
	provider, ok := r.Controller.Providers.Provider(plan.ExactReference.ProviderID)
	if !ok {
		return r.recoveryDecision(ctx, state, "local_proxy_provider_unavailable")
	}
	switch lease.State {
	case hostresources.EndpointLeaseReserved:
		// A RESERVED local-proxy lease proves that apply never crossed the
		// provider mutation boundary. Reconciliation may release it, but it
		// never turns PREPARED into applied.
		released, releaseErr := provider.ReleaseLocalProxyGuardLease(ctx, hostresources.ReleaseLocalProxyGuardLeaseRequestV1{
			RequestID: hostresources.Revision(struct{ Operation, Action string }{operation.OperationID, "restart_cancel_prepared"}),
			LeaseID:   lease.LeaseID, ExpectedRevision: lease.LeaseRevision,
		})
		if releaseErr != nil {
			return r.recoveryDecision(ctx, state, "local_proxy_reserved_release_ambiguous")
		}
		if err := r.Controller.persistState(ctx, plan, operation, released, StateNotApplied, "", nil, false); err != nil {
			return protectionoperations.ReconcileDecision{}, err
		}
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateCancelled, Reason: "local_proxy_cancelled_before_apply_boundary"}, nil
	case hostresources.EndpointLeaseReleased:
		if err := r.Controller.persistState(ctx, plan, operation, lease, StateNotApplied, "", nil, false); err != nil {
			return protectionoperations.ReconcileDecision{}, err
		}
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateCancelled, Reason: "local_proxy_provider_guard_released"}, nil
	case hostresources.EndpointLeaseMutationPending, hostresources.EndpointLeaseActive:
	default:
		return r.recoveryDecision(ctx, state, "local_proxy_lease_state_unknown")
	}
	if _, err := r.Controller.Providers.ResolveV1(ctx, *plan.ExactReference, r.Controller.currentTime()); err != nil {
		return r.recoveryDecision(ctx, state, "local_proxy_fact_revalidation_failed")
	}
	marker := hostresources.Revision(struct {
		Schema, Operation, Plan, Lease string
		Revision                       int
		Boundary                       int64
	}{
		"solovey-ui/local-proxy-restart-health-marker/v1", operation.OperationID, plan.PlanDigest,
		lease.LeaseRevision, operation.Revision, time.Now().UTC().UnixNano(),
	})
	if err := r.Controller.persistState(ctx, plan, operation, lease, StateHealth, marker, nil, true); err != nil {
		return protectionoperations.ReconcileDecision{}, err
	}
	state, err = r.Controller.Repository.LocalProxyStateByOperation(ctx, operation.OperationID)
	if err != nil {
		return protectionoperations.ReconcileDecision{}, err
	}
	boundary := time.Now().UTC().UnixNano()
	health, err := r.Controller.probeAll(ctx, plan, operation, lease, marker, boundary)
	if err != nil {
		return r.recoveryDecision(ctx, state, "local_proxy_restart_health_failed")
	}
	if _, err := r.Controller.Providers.ResolveV1(ctx, *plan.ExactReference, r.Controller.currentTime()); err != nil {
		return r.recoveryDecision(ctx, state, "local_proxy_restart_fact_drift")
	}
	if lease.State == hostresources.EndpointLeaseMutationPending {
		lease, err = provider.ActivateLocalProxyGuardLease(ctx, hostresources.MutateLocalProxyGuardLeaseRequestV1{
			RequestID: hostresources.Revision(struct{ Operation, Marker string }{operation.OperationID, marker}),
			LeaseID:   lease.LeaseID, ExpectedRevision: lease.LeaseRevision,
		})
		if err != nil {
			return r.recoveryDecision(ctx, state, "local_proxy_restart_activation_ambiguous")
		}
	} else if lease.ExpiresAt <= r.Controller.currentTime().Add(5*time.Minute).Unix() {
		lease, err = provider.RenewLocalProxyGuardLease(ctx, hostresources.MutateLocalProxyGuardLeaseRequestV1{
			RequestID: hostresources.Revision(struct {
				Operation string
				At        int64
			}{operation.OperationID, r.Controller.currentTime().Unix()}),
			LeaseID: lease.LeaseID, ExpectedRevision: lease.LeaseRevision,
			FreshnessSeconds: uint32(hostresources.MaxLocalProxyLeaseFreshnessV1 / time.Second),
		})
		if err != nil {
			return r.recoveryDecision(ctx, state, "local_proxy_restart_renewal_ambiguous")
		}
	}
	if err := r.Controller.persistState(ctx, plan, operation, lease, StateAppliedExperimental, marker, health, false); err != nil {
		return protectionoperations.ReconcileDecision{}, err
	}
	return protectionoperations.ReconcileDecision{State: protectionoperations.StateApplied, Reason: "local_proxy_exact_guard_and_health_reverified"}, nil
}

func (r Reconciler) reconcileWithoutMirror(ctx context.Context, operation protectionrepository.OperationLockModel) (protectionoperations.ReconcileDecision, error) {
	leases, err := r.Controller.Providers.LeasesByHolderV1(ctx, operation.OperationID)
	if err != nil || len(leases) != 1 {
		return protectionoperations.ReconcileDecision{
			State: protectionoperations.StateReconcileRequired, Reason: "local_proxy_mirror_or_authority_ambiguous",
		}, nil
	}
	lease := leases[0]
	provider, ok := r.Controller.Providers.Provider(lease.AuthorityProviderID)
	if !ok || lease.State != hostresources.EndpointLeaseReserved {
		return protectionoperations.ReconcileDecision{
			State: protectionoperations.StateReconcileRequired, Reason: "local_proxy_authority_retained_without_mirror",
		}, nil
	}
	_, err = provider.ReleaseLocalProxyGuardLease(ctx, hostresources.ReleaseLocalProxyGuardLeaseRequestV1{
		RequestID: hostresources.Revision(struct{ Operation, Action string }{operation.OperationID, "restart_orphan_reserved"}),
		LeaseID:   lease.LeaseID, ExpectedRevision: lease.LeaseRevision,
	})
	if err != nil {
		return protectionoperations.ReconcileDecision{
			State: protectionoperations.StateReconcileRequired, Reason: "local_proxy_orphan_reserved_release_ambiguous",
		}, nil
	}
	return protectionoperations.ReconcileDecision{State: protectionoperations.StateCancelled, Reason: "local_proxy_orphan_reserved_released"}, nil
}

func (r Reconciler) recoveryDecision(ctx context.Context, state protectionrepository.LocalProxyStateV1Model, reason string) (protectionoperations.ReconcileDecision, error) {
	state.ActualState, state.RecoveryRequired = string(StateRecoveryRequired), true
	if err := r.Controller.Repository.SaveLocalProxyState(ctx, state); err != nil {
		return protectionoperations.ReconcileDecision{}, err
	}
	return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: reason}, nil
}

var _ protectionoperations.Reconciler = Reconciler{}
