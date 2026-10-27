package coreinboundcontrol

import coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"

type engineFact struct {
	Module, Version, Sum, SourceRevision string
}

// Source recognition belongs to core/provider. This owner separately qualifies
// the existing bounded fallback shapes on exact maintained sources. A new pin
// requires repeating the actual TLS fallback and mutation/regression proofs;
// merely adding a research source to the provider catalog cannot enable it.
func fallbackSourceQualified(source coreprovider.Identity) bool {
	if source.State != coreprovider.Verified {
		return false
	}
	switch source.SourceRevision {
	case "8b914e4be5103d5f343dac94f26c3ebeeb226ffc":
		return source.ID == "sing-box"
	case "45a61c142c496e305f2bc0f8ce4915aa7f7dcd3d":
		return source.ID == "shtorm-extended"
	default:
		return false
	}
}

func resolveEngineFact(module RuntimeModuleV1) (engineFact, []ReasonCode) {
	if module.Path != PinnedSingBoxModule {
		return engineFact{}, []ReasonCode{ReasonSingBoxModuleMissing}
	}
	if module.Replaced || module.Replacement != nil {
		replacement := module.Replacement
		if !module.Replaced || replacement == nil || replacement.Replaced || replacement.Replacement != nil {
			return engineFact{}, []ReasonCode{ReasonSingBoxModuleReplaced}
		}
		source := observedProviderSource(replacement.Path, replacement.Version, replacement.Sum)
		if !fallbackSourceQualified(source) {
			return engineFact{}, []ReasonCode{ReasonSingBoxSourceUnqualified}
		}
		if module.Version != "v"+source.RuntimeSemanticVersion {
			return engineFact{}, []ReasonCode{ReasonSingBoxVersionMismatch}
		}
		return engineFact{source.Module, source.ProviderVersion, source.ModuleSum, source.SourceRevision}, nil
	}
	reasons := []ReasonCode{}
	if module.Version != PinnedSingBoxVersion {
		reasons = append(reasons, ReasonSingBoxVersionMismatch)
	}
	if module.Sum != PinnedSingBoxModuleSum {
		reasons = append(reasons, ReasonSingBoxSumMismatch)
	}
	if len(reasons) != 0 {
		return engineFact{}, reasons
	}
	return engineFact{PinnedSingBoxModule, PinnedSingBoxVersion, PinnedSingBoxModuleSum, PinnedSingBoxSourceRevision}, nil
}

func observedProviderSource(module, version, sum string) coreprovider.Identity {
	return coreprovider.ResolveModule(module, version, sum)
}

// RuntimeIdentityVerifiedV1 revalidates source qualification, the uTLS/build
// profile and the complete identity digest. Consumers do not repeat source pins
// or treat a caller's verified label/nonempty revision as sufficient proof.
func RuntimeIdentityVerifiedV1(identity CoreRuntimeIdentityV1) bool {
	if identity.Schema != RuntimeIdentitySchemaV1 || identity.State != RuntimeIdentityVerified || len(identity.ReasonCodes) != 0 ||
		identity.BuildProfileRevision != expectedBuildProfileRevision(identity.WithUTLS) ||
		identity.CapabilityResolverRevision != CapabilityResolverRevisionV1 || identity.IdentityRevision != runtimeIdentityRevision(identity) {
		return false
	}
	legacySource := identity.SingBoxModule == PinnedSingBoxModule && identity.SingBoxVersion == PinnedSingBoxVersion &&
		identity.SingBoxModuleSum == PinnedSingBoxModuleSum && identity.SingBoxSourceRevision == PinnedSingBoxSourceRevision
	if !legacySource {
		source := observedProviderSource(identity.SingBoxModule, identity.SingBoxVersion, identity.SingBoxModuleSum)
		if !fallbackSourceQualified(source) || source.SourceRevision != identity.SingBoxSourceRevision {
			return false
		}
	}
	utlsAbsent := identity.UTLSModule == "" && identity.UTLSVersion == "" && identity.UTLSModuleSum == "" && identity.UTLSSourceRevision == ""
	return !identity.WithUTLS && utlsAbsent || identity.UTLSModule == PinnedUTLSModule && identity.UTLSVersion == PinnedUTLSVersion &&
		identity.UTLSModuleSum == PinnedUTLSModuleSum && identity.UTLSSourceRevision == PinnedUTLSSourceRevision
}
