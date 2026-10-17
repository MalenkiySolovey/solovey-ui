package deployment

import "time"

const PlatformFactsSchemaV1 = "solovey-ui/platform-facts/v1"

// PlatformFact describes availability at the selected provider boundary. It is
// never an authorization grant, a readiness result, or a substitute for doctor.
type PlatformFact struct {
	Availability Availability `json:"availability"`
	Backend      string       `json:"backend"`
	Reason       string       `json:"reason"`
}

// PlatformSandboxFacts contains observed unit configuration, not proof of an
// arbitrary process's effective privileges or permission to change them.
type PlatformSandboxFacts struct {
	PlatformFact
	NoNewPrivileges      *bool    `json:"noNewPrivileges,omitempty"`
	BoundingCapabilities []string `json:"boundingCapabilities,omitempty"`
	AmbientCapabilities  []string `json:"ambientCapabilities,omitempty"`
	EvidenceRevision     string   `json:"evidenceRevision,omitempty"`
}

// PlatformFacts is a bounded Local Host projection of deployment evidence.
// Posture keeps its released serialization and revision inputs unchanged.
// Storage/package/network hashes do not establish capabilities in those areas.
type PlatformFacts struct {
	Schema             string               `json:"schema"`
	HostScope          string               `json:"hostScope"`
	Source             string               `json:"source"`
	EvidenceRevision   string               `json:"evidenceRevision,omitempty"`
	GeneratedAt        int64                `json:"generatedAt"`
	ObservedAt         int64                `json:"observedAt,omitempty"`
	ExpiresAt          int64                `json:"expiresAt,omitempty"`
	ServiceSupervision PlatformFact         `json:"serviceSupervision"`
	PrivilegeExecution PlatformFact         `json:"privilegeExecution"`
	PersistentStorage  PlatformFact         `json:"persistentStorage"`
	PackageOperations  PlatformFact         `json:"packageOperations"`
	UpdateLifecycle    PlatformFact         `json:"updateLifecycle"`
	NetworkObservation PlatformFact         `json:"networkObservation"`
	Sandbox            PlatformSandboxFacts `json:"sandbox"`
	Revision           string               `json:"revision"`
}

// UnknownPlatformFacts deliberately accepts only a safe bounded reason. Raw
// provider errors, paths, environment and credentials must not enter this DTO.
func UnknownPlatformFacts(reason string, now time.Time) PlatformFacts {
	if !safeCode(reason, 96) {
		reason = "platform_evidence_unavailable"
	}
	unknown := PlatformFact{Unknown, "unknown", reason}
	result := PlatformFacts{Schema: PlatformFactsSchemaV1, HostScope: "LOCAL", Source: "unavailable",
		GeneratedAt: now.Unix(), ServiceSupervision: unknown, PrivilegeExecution: unknown,
		PersistentStorage: unknown, PackageOperations: unknown, UpdateLifecycle: unknown, NetworkObservation: unknown,
		Sandbox: PlatformSandboxFacts{PlatformFact: unknown}}
	result.Revision = Revision(result)
	return result
}

// ProjectPlatformFacts only recognizes the already selected, closed provider
// contracts. A familiar runtime/profile supplied by an unknown provider is not
// evidence. Native root identity never supplies a broker fallback.
func ProjectPlatformFacts(posture Posture, providerID string, now time.Time) PlatformFacts {
	var backend string
	switch {
	case providerID == ProviderV1 && posture.Runtime == RuntimeSystemdNative:
		backend = "systemd"
	case providerID == "package-managed-openwrt-posture/v1" && posture.Runtime == RuntimePackageManaged:
		backend = "procd"
	case providerID == "local-docker-posture/v1" && posture.Runtime == RuntimeDocker:
		backend = "container-runtime"
	default:
		return UnknownPlatformFacts("platform_provider_unrecognized", now)
	}
	if posture.ObservedAt > now.Unix() || posture.ValidateProjection(now) != nil {
		return UnknownPlatformFacts("platform_evidence_invalid_or_stale", now)
	}
	result := UnknownPlatformFacts("platform_fact_not_observed", now)
	result.Source, result.EvidenceRevision = providerID, posture.Revision
	result.ObservedAt, result.ExpiresAt = posture.ObservedAt, posture.ExpiresAt
	result.ServiceSupervision = PlatformFact{Unknown, backend, "supervision_not_observed"}
	if posture.Runtime != RuntimeDocker && posture.InstalledProfile != "" {
		result.ServiceSupervision = PlatformFact{Available, backend, "installed_supervision_contract_observed"}
	}
	if posture.Runtime == RuntimeSystemdNative && posture.Systemd.LoadState != "loaded" {
		result.ServiceSupervision = PlatformFact{Unavailable, backend, "service_unit_not_loaded"}
	}
	result.PrivilegeExecution = PlatformFact{Unavailable, "closed-broker", "privileged_broker_unavailable"}
	if posture.Runtime != RuntimeDocker && posture.BrokerAvailable {
		result.PrivilegeExecution = PlatformFact{Unknown, "closed-broker", "broker_evidence_invalid"}
		if digest(posture.BrokerRevision) {
			result.PrivilegeExecution = PlatformFact{Available, "closed-broker", "closed_broker_observed_not_operation_authority"}
		}
	}
	result.PersistentStorage.Reason = "persistent_mounts_not_observed"
	result.PackageOperations.Reason = "package_backend_and_operations_not_observed"
	result.NetworkObservation.Reason = "network_inventory_not_in_posture"
	if posture.Runtime == RuntimeSystemdNative {
		unit := posture.Systemd
		nnp := unit.NoNewPrivileges
		result.Sandbox = PlatformSandboxFacts{
			PlatformFact:    PlatformFact{Available, "systemd", "unit_configuration_observed_not_effective_process_authority"},
			NoNewPrivileges: &nnp, BoundingCapabilities: append([]string(nil), unit.BoundingCapabilities...),
			AmbientCapabilities: append([]string(nil), unit.AmbientCapabilities...), EvidenceRevision: unit.Revision}
	}
	result.Revision = ""
	result.Revision = Revision(result)
	return result
}
