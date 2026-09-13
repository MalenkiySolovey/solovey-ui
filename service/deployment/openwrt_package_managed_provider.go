package deployment

import (
	"context"
	"fmt"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

const OpenWrtPackageManagedDeploymentKind = "openwrt-package-managed"

type OpenWrtPackageManagedProvider struct {
	now         func() time.Time
	loadOwner   func() (deploymentidentity.ApplicationOwnerContractProcdV1, error)
	brokerProbe func(context.Context) (string, bool)
}

func init() {
	registerRuntimeProvider(OpenWrtPackageManagedDeploymentKind, func() Provider { return NewOpenWrtPackageManagedProvider() })
}

func NewOpenWrtPackageManagedProvider() *OpenWrtPackageManagedProvider {
	return &OpenWrtPackageManagedProvider{now: time.Now, loadOwner: deploymentidentity.LoadBoundProcdInstalled, brokerProbe: probeOpenWrtPackageManagedBroker}
}

func (*OpenWrtPackageManagedProvider) ProviderID() string {
	return "package-managed-openwrt-posture/v1"
}
func (*OpenWrtPackageManagedProvider) UpdateLifecycle() UpdateLifecycle {
	return UpdateLifecyclePackageManaged
}

func (p *OpenWrtPackageManagedProvider) BrokerPresentation(ctx context.Context) BrokerPresentation {
	_, available := p.probeBroker(ctx)
	return BrokerPresentation{Available: available, ProtocolRevision: broker.CapabilityRevision,
		Transport: "procd-supervised-standalone-unix-peer-credentials", PeerPosture: "root-owned-package-manifest"}
}

func (*OpenWrtPackageManagedProvider) Capabilities(context.Context) domain.Capabilities {
	result := domain.Capabilities{Observe: domain.Available, Doctor: domain.Available, Migrate: domain.Unavailable,
		Rollback: domain.Unavailable, Reasons: []string{"package_manager_owns_deployment_lifecycle"}}
	result.Revision = domain.Revision(result)
	return result
}

func (p *OpenWrtPackageManagedProvider) Observe(ctx context.Context) (domain.Posture, error) {
	if p == nil || p.loadOwner == nil {
		return domain.Posture{}, ErrProviderUnavailable
	}
	proof, err := p.loadOwner()
	if err != nil {
		return domain.Posture{}, fmt.Errorf("%w: installed owner contract", ErrPackagePosture)
	}
	owner, err := deploymentidentity.ExpectedProcdApplicationOwner(proof)
	if err != nil {
		return domain.Posture{}, fmt.Errorf("%w: owner projection", ErrPackagePosture)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if p.now != nil {
		now = p.now().UTC().Truncate(time.Second)
	}
	brokerRevision, brokerAvailable := p.probeBroker(ctx)
	reasons := []string{"package_managed_active_state_not_lifecycle_mutation_authority"}
	if !brokerAvailable {
		reasons = append(reasons, "privileged_broker_unavailable")
	}
	posture := domain.Posture{
		Schema: domain.SchemaV1, Profile: domain.PackageManagedOpenWrt, InstalledProfile: domain.PackageManagedOpenWrt,
		ActiveProfile: domain.PackageManagedOpenWrt, Runtime: domain.RuntimePackageManaged,
		PanelUID: owner.ProcessUID, PanelGID: owner.ProcessGID, BrokerAvailable: brokerAvailable,
		BrokerRevision: brokerRevision, ServiceRevision: owner.ContractRevision,
		DataRevision: domain.Revision(struct{ InstanceID, DeploymentID, RuntimeBinding string }{owner.InstanceID, owner.DeploymentID, owner.RuntimeRootBindingRevision}),
		HardeningRevision: domain.Revision(struct {
			ServiceIdentity, ExecutableSHA256 string
			UID, GID                          uint32
		}{owner.ServiceIdentity, owner.ExecutableSHA256, owner.ProcessUID, owner.ProcessGID}),
		ObservedAt: now.Unix(), ExpiresAt: now.Add(2 * time.Minute).Unix(), Reasons: reasons,
	}
	domain.SetPostureRevision(&posture)
	return posture, nil
}

func (p *OpenWrtPackageManagedProvider) Doctor(ctx context.Context) (domain.DoctorReport, error) {
	posture, err := p.Observe(ctx)
	report := domain.DoctorReport{Capabilities: p.Capabilities(ctx), GeneratedAt: time.Now().UTC().Unix()}
	if p.now != nil {
		report.GeneratedAt = p.now().UTC().Unix()
	}
	if err != nil {
		report.Findings = append(report.Findings, domain.Finding{Code: "package_managed_posture_unavailable", Severity: domain.SeverityCritical,
			MessageKey: "deployment.doctor.packageManagedPostureUnavailable", Remediation: "repair the installed package-owned deployment contracts"})
		return domain.FinalizeDoctor(report), nil
	}
	report.Posture = &posture
	report.Findings = append(report.Findings, domain.Finding{Code: "package_manager_owns_deployment_lifecycle", Severity: domain.SeverityInfo,
		MessageKey: "deployment.doctor.packageManagerOwnsLifecycle", Remediation: "use the OpenWrt package manager for install, update, rollback, and removal"})
	if !posture.BrokerAvailable {
		report.Findings = append(report.Findings, domain.Finding{Code: "privileged_broker_unavailable", Severity: domain.SeverityWarning,
			MessageKey: "deployment.doctor.privilegedBrokerUnavailable", Remediation: "repair the package-owned procd broker service and its manifest"})
	}
	return domain.FinalizeDoctor(report), nil
}

func (p *OpenWrtPackageManagedProvider) probeBroker(ctx context.Context) (string, bool) {
	if p == nil || p.brokerProbe == nil {
		return "", false
	}
	return p.brokerProbe(ctx)
}

func probeOpenWrtPackageManagedBroker(ctx context.Context) (string, bool) {
	probeContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var capabilities broker.CapabilitiesV1
	client := broker.NewClient(broker.RolePanel)
	_, err := client.Invoke(probeContext, broker.Call{Verb: broker.VerbCapabilities, OperationID: "deployment-package-broker",
		Purpose: "deployment-read-only-posture", Timeout: 2 * time.Second, Payload: struct{}{}}, &capabilities)
	if err != nil || capabilities.ProtocolVersion != broker.ProtocolVersion || capabilities.CapabilityRevision != broker.CapabilityRevision ||
		capabilities.Role != broker.RolePanel || capabilities.Revision == "" {
		return "", false
	}
	return capabilities.Revision, true
}

func (*OpenWrtPackageManagedProvider) Prepare(context.Context, FenceV1, domain.ProfileID) (string, error) {
	return "", ErrPackageManaged
}
func (*OpenWrtPackageManagedProvider) Apply(context.Context, FenceV1, domain.ProfileID, string) error {
	return ErrPackageManaged
}
func (*OpenWrtPackageManagedProvider) Verify(context.Context, FenceV1, domain.ProfileID, string) (domain.Posture, error) {
	return domain.Posture{}, ErrPackageManaged
}
func (*OpenWrtPackageManagedProvider) Rollback(context.Context, FenceV1, domain.ProfileID, string) (domain.Posture, error) {
	return domain.Posture{}, ErrPackageManaged
}
