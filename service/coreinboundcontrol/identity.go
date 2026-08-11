package coreinboundcontrol

import (
	"runtime/debug"
	"sort"
)

func ReadQUICBuildFeatureV1(identity CoreRuntimeIdentityV1) QUICBuildFeatureV1 {
	state := BuildFeatureUnavailable
	reasons := []ReasonCode(nil)
	if identity.State != RuntimeIdentityVerified {
		state = BuildFeatureUnknown
		reasons = append(reasons, ReasonRuntimeIdentityUnverified)
	} else if compiledWithQUIC {
		state = BuildFeatureSupported
	}
	value := QUICBuildFeatureV1{
		Schema: QUICBuildFeatureSchemaV1, Feature: "with_quic", State: state,
		RuntimeIdentity: identity.IdentityRevision, SourceRevision: PinnedSingBoxSourceRevision,
		ModuleRevision: digestValue(struct{ Module, Version, Sum string }{PinnedSingBoxModule, PinnedSingBoxVersion, PinnedSingBoxModuleSum}),
		BuildProfileRevision: digestValue(struct {
			Schema   string
			WithQUIC bool
		}{"solovey-ui/core-quic-build-profile/v1", compiledWithQUIC}),
		ObservationMethod: "compile_tag_and_debug_build_info", ReasonCodes: normalizedReasons(reasons),
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
	input := RuntimeBuildInputV1{
		WithUTLS:                   compiledWithUTLS,
		BuildProfileRevision:       expectedBuildProfileRevision(compiledWithUTLS),
		CapabilityResolverRevision: CapabilityResolverRevisionV1,
	}
	info, ok := debug.ReadBuildInfo()
	input.Available = ok
	if ok {
		input.Modules = make([]RuntimeModuleV1, 0, len(info.Deps))
		for _, dependency := range info.Deps {
			if dependency == nil || dependency.Path != PinnedSingBoxModule && dependency.Path != PinnedUTLSModule {
				continue
			}
			input.Modules = append(input.Modules, RuntimeModuleV1{
				Path: dependency.Path, Version: dependency.Version, Sum: dependency.Sum, Replaced: dependency.Replace != nil,
			})
		}
	}
	return ResolveRuntimeIdentityV1(input)
}

func ResolveRuntimeIdentityV1(input RuntimeBuildInputV1) CoreRuntimeIdentityV1 {
	reasons := make([]ReasonCode, 0, 8)
	modules := make(map[string]RuntimeModuleV1, len(input.Modules))
	for _, module := range input.Modules {
		modules[module.Path] = module
	}
	if !input.Available {
		reasons = append(reasons, ReasonBuildInfoMissing)
	}
	checkModule := func(path, version, sum string, missing, replaced, versionMismatch, sumMismatch ReasonCode) {
		module, ok := modules[path]
		if !ok {
			reasons = append(reasons, missing)
			return
		}
		if module.Replaced {
			reasons = append(reasons, replaced)
		}
		if module.Version != version {
			reasons = append(reasons, versionMismatch)
		}
		if module.Sum != sum {
			reasons = append(reasons, sumMismatch)
		}
	}
	checkModule(PinnedSingBoxModule, PinnedSingBoxVersion, PinnedSingBoxModuleSum,
		ReasonSingBoxModuleMissing, ReasonSingBoxModuleReplaced, ReasonSingBoxVersionMismatch, ReasonSingBoxSumMismatch)
	checkModule(PinnedUTLSModule, PinnedUTLSVersion, PinnedUTLSModuleSum,
		ReasonUTLSModuleMissing, ReasonUTLSModuleReplaced, ReasonUTLSVersionMismatch, ReasonUTLSSumMismatch)
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
		SingBoxModule: PinnedSingBoxModule, SingBoxVersion: moduleValue(modules, PinnedSingBoxModule).Version,
		SingBoxModuleSum: moduleValue(modules, PinnedSingBoxModule).Sum, SingBoxSourceRevision: PinnedSingBoxSourceRevision,
		UTLSModule: PinnedUTLSModule, UTLSVersion: moduleValue(modules, PinnedUTLSModule).Version,
		UTLSModuleSum: moduleValue(modules, PinnedUTLSModule).Sum, UTLSSourceRevision: PinnedUTLSSourceRevision,
		WithUTLS: input.WithUTLS, BuildProfileRevision: input.BuildProfileRevision,
		CapabilityResolverRevision: input.CapabilityResolverRevision, ReasonCodes: reasons,
	}
	identity.IdentityRevision = digestValue(struct {
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
		identity.CapabilityResolverRevision, identity.ReasonCodes})
	return identity
}

func expectedBuildProfileRevision(withUTLS bool) string {
	if withUTLS {
		return BuildProfileWithUTLSRevision
	}
	return BuildProfileWithoutUTLSRevision
}

func moduleValue(modules map[string]RuntimeModuleV1, path string) RuntimeModuleV1 {
	return modules[path]
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
