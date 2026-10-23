package coreinboundcontrol

import (
	"runtime/debug"
	"sort"
)

func ReadQUICBuildFeatureV1(identity CoreRuntimeIdentityV1) QUICBuildFeatureV1 {
	state := BuildFeatureUnavailable
	reasons := []ReasonCode(nil)
	verified := RuntimeIdentityVerifiedV1(identity)
	if !verified {
		state = BuildFeatureUnknown
		reasons = append(reasons, ReasonRuntimeIdentityUnverified)
	} else if compiledWithQUIC {
		state = BuildFeatureSupported
	}
	value := QUICBuildFeatureV1{
		Schema: QUICBuildFeatureSchemaV1, Feature: "with_quic", State: state,
		RuntimeIdentity: identity.IdentityRevision,
		BuildProfileRevision: digestValue(struct {
			Schema   string
			WithQUIC bool
		}{"solovey-ui/core-quic-build-profile/v1", compiledWithQUIC}),
		ObservationMethod: "compile_tag_and_debug_build_info", ReasonCodes: normalizedReasons(reasons),
	}
	if verified {
		value.SourceRevision = identity.SingBoxSourceRevision
		value.ModuleRevision = digestValue(struct{ Module, Version, Sum string }{identity.SingBoxModule, identity.SingBoxVersion, identity.SingBoxModuleSum})
	}
	value.Revision = digestValue(struct {
		Schema, Feature                                                                          string
		State                                                                                    BuildFeatureState
		RuntimeIdentity, SourceRevision, ModuleRevision, BuildProfileRevision, ObservationMethod string
		ReasonCodes                                                                              []ReasonCode
	}{value.Schema, value.Feature, value.State, value.RuntimeIdentity, value.SourceRevision, value.ModuleRevision, value.BuildProfileRevision, value.ObservationMethod, value.ReasonCodes})
	return value
}

func ReadRuntimeIdentityV1() CoreRuntimeIdentityV1 {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		info = nil
	}
	return ResolveRuntimeIdentityV1(runtimeInputFromBuildInfo(info))
}

func runtimeInputFromBuildInfo(info *debug.BuildInfo) RuntimeBuildInputV1 {
	input := RuntimeBuildInputV1{
		WithUTLS:                   compiledWithUTLS,
		BuildProfileRevision:       expectedBuildProfileRevision(compiledWithUTLS),
		CapabilityResolverRevision: CapabilityResolverRevisionV1,
	}
	input.Available = info != nil
	if info != nil {
		input.Modules = make([]RuntimeModuleV1, 0, len(info.Deps))
		for _, dependency := range info.Deps {
			if dependency == nil || dependency.Path != PinnedSingBoxModule && dependency.Path != PinnedUTLSModule {
				continue
			}
			module := RuntimeModuleV1{
				Path: dependency.Path, Version: dependency.Version, Sum: dependency.Sum, Replaced: dependency.Replace != nil,
			}
			if replacement := dependency.Replace; replacement != nil {
				module.Replacement = &RuntimeModuleV1{Path: replacement.Path, Version: replacement.Version,
					Sum: replacement.Sum, Replaced: replacement.Replace != nil}
			}
			input.Modules = append(input.Modules, module)
		}
	}
	return input
}

func ResolveRuntimeIdentityV1(input RuntimeBuildInputV1) CoreRuntimeIdentityV1 {
	reasons := make([]ReasonCode, 0, 8)
	modules := make(map[string]RuntimeModuleV1, len(input.Modules))
	for _, module := range input.Modules {
		if _, duplicate := modules[module.Path]; duplicate {
			switch module.Path {
			case PinnedSingBoxModule:
				reasons = append(reasons, ReasonSingBoxModuleAmbiguous)
			case PinnedUTLSModule:
				reasons = append(reasons, ReasonUTLSModuleAmbiguous)
			}
		}
		modules[module.Path] = module
	}
	if !input.Available {
		reasons = append(reasons, ReasonBuildInfoMissing)
	}
	engine, engineReasons := resolveEngineFact(modules[PinnedSingBoxModule])
	reasons = append(reasons, engineReasons...)
	utls := modules[PinnedUTLSModule]
	utlsValid := utls.Path == PinnedUTLSModule && !utls.Replaced && utls.Replacement == nil && utls.Version == PinnedUTLSVersion && utls.Sum == PinnedUTLSModuleSum
	if utls.Path == "" {
		if input.WithUTLS {
			reasons = append(reasons, ReasonUTLSModuleMissing)
		}
	} else {
		if utls.Replaced || utls.Replacement != nil {
			reasons = append(reasons, ReasonUTLSModuleReplaced)
		}
		if utls.Version != PinnedUTLSVersion {
			reasons = append(reasons, ReasonUTLSVersionMismatch)
		}
		if utls.Sum != PinnedUTLSModuleSum {
			reasons = append(reasons, ReasonUTLSSumMismatch)
		}
	}
	expectedProfile := expectedBuildProfileRevision(input.WithUTLS)
	if input.BuildProfileRevision == "" {
		reasons = append(reasons, ReasonBuildProfileUnknown)
	} else if input.BuildProfileRevision != expectedProfile {
		reasons = append(reasons, ReasonWithUTLSInconsistent)
	}
	if input.CapabilityResolverRevision != CapabilityResolverRevisionV1 {
		reasons = append(reasons, ReasonResolverRevisionMismatch)
	}
	reasons = normalizedReasons(reasons)
	state := RuntimeIdentityVerified
	if len(reasons) != 0 {
		state = RuntimeIdentityUnknown
	}
	identity := CoreRuntimeIdentityV1{
		Schema: RuntimeIdentitySchemaV1, State: state,
		SingBoxModule: engine.Module, SingBoxVersion: engine.Version,
		SingBoxModuleSum: engine.Sum, SingBoxSourceRevision: engine.SourceRevision,
		WithUTLS: input.WithUTLS, BuildProfileRevision: input.BuildProfileRevision,
		CapabilityResolverRevision: input.CapabilityResolverRevision, ReasonCodes: reasons,
	}
	if utlsValid {
		identity.UTLSModule, identity.UTLSVersion, identity.UTLSModuleSum, identity.UTLSSourceRevision = PinnedUTLSModule, PinnedUTLSVersion, PinnedUTLSModuleSum, PinnedUTLSSourceRevision
	}
	identity.IdentityRevision = runtimeIdentityRevision(identity)
	return identity
}

func runtimeIdentityRevision(identity CoreRuntimeIdentityV1) string {
	return digestValue(struct {
		Schema                     string
		State                      RuntimeIdentityState
		SingBoxModule              string
		SingBoxVersion             string
		SingBoxModuleSum           string
		SingBoxSourceRevision      string
		UTLSModule                 string
		UTLSVersion                string
		UTLSModuleSum              string
		UTLSSourceRevision         string
		WithUTLS                   bool
		BuildProfileRevision       string
		CapabilityResolverRevision string
		ReasonCodes                []ReasonCode
	}{identity.Schema, identity.State, identity.SingBoxModule, identity.SingBoxVersion, identity.SingBoxModuleSum,
		identity.SingBoxSourceRevision, identity.UTLSModule, identity.UTLSVersion, identity.UTLSModuleSum,
		identity.UTLSSourceRevision, identity.WithUTLS, identity.BuildProfileRevision,
		identity.CapabilityResolverRevision, normalizedReasons(identity.ReasonCodes)})
}

func expectedBuildProfileRevision(withUTLS bool) string {
	if withUTLS {
		return BuildProfileWithUTLSRevision
	}
	return BuildProfileWithoutUTLSRevision
}

func normalizedReasons(values []ReasonCode) []ReasonCode {
	seen := make(map[ReasonCode]struct{}, len(values))
	result := make([]ReasonCode, 0, min(len(values), 32))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == 32 {
			break
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
