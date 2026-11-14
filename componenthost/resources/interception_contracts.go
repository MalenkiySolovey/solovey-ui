package resources

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	time "time"
)

const InterceptionFactSchemaV1 = canonical.InterceptionFactSchemaV1
const InterceptionReferenceSchemaV1 = canonical.InterceptionReferenceSchemaV1
const InterceptionLeaseSchemaV1 = canonical.InterceptionLeaseSchemaV1
const IngressScopeFactSchemaV1 = canonical.IngressScopeFactSchemaV1
const IngressScopeReferenceSchemaV1 = canonical.IngressScopeReferenceSchemaV1
const InterceptionProviderRevisionV1 = canonical.InterceptionProviderRevisionV1
const IngressScopeProviderRevisionV1 = canonical.IngressScopeProviderRevisionV1
const MaxInterceptionFreshnessV1 = canonical.MaxInterceptionFreshnessV1
const MaxInterceptionLeaseFreshnessV1 = canonical.MaxInterceptionLeaseFreshnessV1
const MaxInterceptionReasonCodesV1 = canonical.MaxInterceptionReasonCodesV1
const MaxInterceptionLeasePageV1 = canonical.MaxInterceptionLeasePageV1

var ErrInterceptionLeaseConflictV1 = canonical.ErrInterceptionLeaseConflictV1

type InterceptionKindV1 = canonical.InterceptionKindV1

const InterceptionRedirectV1 = canonical.InterceptionRedirectV1
const InterceptionTProxyV1 = canonical.InterceptionTProxyV1
const InterceptionTUNV1 = canonical.InterceptionTUNV1

type InterceptionOwnershipV1 = canonical.InterceptionOwnershipV1

const InterceptionProviderManagedV1 = canonical.InterceptionProviderManagedV1
const InterceptionExternalManagedV1 = canonical.InterceptionExternalManagedV1
const InterceptionOwnershipUnknownV1 = canonical.InterceptionOwnershipUnknownV1

type InterceptionListenerStateV1 = canonical.InterceptionListenerStateV1

const InterceptionListenerObservedExactV1 = canonical.InterceptionListenerObservedExactV1
const InterceptionListenerUnobservedV1 = canonical.InterceptionListenerUnobservedV1
const InterceptionListenerStaleV1 = canonical.InterceptionListenerStaleV1
const InterceptionListenerForeignV1 = canonical.InterceptionListenerForeignV1
const InterceptionListenerUnknownV1 = canonical.InterceptionListenerUnknownV1

type InterceptionInboundFactV1 = canonical.InterceptionInboundFactV1
type InterceptionReferenceV1 = canonical.InterceptionReferenceV1
type InterceptionLeaseV1 = canonical.InterceptionLeaseV1
type AcquireInterceptionLeaseRequestV1 = canonical.AcquireInterceptionLeaseRequestV1
type MutateInterceptionLeaseRequestV1 = canonical.MutateInterceptionLeaseRequestV1
type ReleaseInterceptionLeaseRequestV1 = canonical.ReleaseInterceptionLeaseRequestV1
type GetInterceptionLeaseRequestV1 = canonical.GetInterceptionLeaseRequestV1
type ListInterceptionLeasesRequestV1 = canonical.ListInterceptionLeasesRequestV1
type InterceptionProviderV1 = canonical.InterceptionProviderV1
type IngressScopeOwnershipV1 = canonical.IngressScopeOwnershipV1

const IngressScopeProviderManagedV1 = canonical.IngressScopeProviderManagedV1
const IngressScopeExternalManagedV1 = canonical.IngressScopeExternalManagedV1
const IngressScopeOwnershipUnknownV1 = canonical.IngressScopeOwnershipUnknownV1

type ForwardedIngressScopeFactV1 = canonical.ForwardedIngressScopeFactV1
type ForwardedIngressScopeReferenceV1 = canonical.ForwardedIngressScopeReferenceV1
type ForwardedIngressScopeProviderV1 = canonical.ForwardedIngressScopeProviderV1

func FinalizeInterceptionFactV1(value InterceptionInboundFactV1) (InterceptionInboundFactV1, error) {
	return canonical.FinalizeInterceptionFactV1(value)
}

func ReferenceInterceptionV1(fact InterceptionInboundFactV1, now time.Time) (InterceptionReferenceV1, error) {
	return canonical.ReferenceInterceptionV1(fact, now)
}

func ResolveExactInterceptionV1(reference InterceptionReferenceV1, fact InterceptionInboundFactV1, now time.Time) error {
	return canonical.ResolveExactInterceptionV1(reference, fact, now)
}

func FinalizeIngressScopeFactV1(value ForwardedIngressScopeFactV1) (ForwardedIngressScopeFactV1, error) {
	return canonical.FinalizeIngressScopeFactV1(value)
}

func ReferenceIngressScopeV1(fact ForwardedIngressScopeFactV1, now time.Time) (ForwardedIngressScopeReferenceV1, error) {
	return canonical.ReferenceIngressScopeV1(fact, now)
}

func ResolveExactIngressScopeV1(reference ForwardedIngressScopeReferenceV1, fact ForwardedIngressScopeFactV1, now time.Time) error {
	return canonical.ResolveExactIngressScopeV1(reference, fact, now)
}

func FinalizeInterceptionLeaseV1(value InterceptionLeaseV1) (InterceptionLeaseV1, error) {
	return canonical.FinalizeInterceptionLeaseV1(value)
}

func ValidateInterceptionLeaseTransitionV1(current, next InterceptionLeaseV1, expected string, mutation EndpointLeaseMutation, now time.Time) error {
	return canonical.ValidateInterceptionLeaseTransitionV1(current, next, expected, mutation, now)
}

func interceptionToken(value string, limit int) bool {
	return canonical.InterceptionContractTokenValidV1(value, limit)
}
