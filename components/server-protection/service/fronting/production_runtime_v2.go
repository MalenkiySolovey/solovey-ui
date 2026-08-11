package fronting

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"time"

	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
)

type staticNginxVersionReaderV2 struct{ observation NginxVersionObservationV2 }

func (r staticNginxVersionReaderV2) ReadNginxVersion(context.Context, string) (NginxVersionObservationV2, error) {
	return r.observation, nil
}

func UnknownNginxRuntimeIdentityV2(now time.Time, managementRevision, reason string) NginxRuntimeIdentityV2 {
	return finalizeRuntimeIdentity(NginxRuntimeIdentityV2{
		Schema: NginxRuntimeIdentitySchemaV2, State: NginxIdentityUnknown, InstallationClass: NginxInstallationUnknown,
		ManagementExclusionsRevision: managementRevision, ObservedAt: now.UTC().Unix(), ExpiresAt: now.UTC().Add(time.Minute).Unix(),
		ReasonCodes: []string{reason},
	})
}

// ObserveManagedRuntimeIdentityV2 composes the existing restricted helper's
// typed capabilities, version observation and active verification into the
// reviewed runtime inspector. It performs no installation or mutation.
func ObserveManagedRuntimeIdentityV2(ctx context.Context, workflow *Workflow, managementRevision string, now time.Time) (NginxRuntimeIdentityV2, error) {
	if workflow == nil || !frontingHexV2(managementRevision) {
		return NginxRuntimeIdentityV2{}, errors.New("nginx_runtime_identity_unavailable")
	}
	capabilities, err := workflow.capabilities(ctx)
	if err != nil || !frontingCapabilitiesAvailable(capabilities) {
		return NginxRuntimeIdentityV2{}, errors.New("nginx_runtime_identity_unavailable")
	}
	correlation := protectionhelper.Correlation{OperationID: "fronting-runtime-identity", InstanceID: workflow.Manager.InstanceID()}
	detected, err := workflow.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation,
		Operation: protectionhelper.OperationNginxDetectVersion, NginxDetectVersion: &protectionhelper.NginxDetectVersionRequest{}})
	if err != nil || detected.NginxVersion == nil || !detected.NginxVersion.Detected {
		return NginxRuntimeIdentityV2{}, errors.New("nginx_runtime_identity_unavailable")
	}
	verified, err := workflow.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation,
		Operation: protectionhelper.OperationNginxVerify, NginxVerify: &protectionhelper.NginxVerifyRequest{
			ExpectedRevision: capabilities.Nginx.ActiveRevision, ExpectedSHA256: capabilities.Nginx.ActiveSHA256,
			ExpectedBinary: capabilities.Nginx.Binary, Listeners: capabilities.Nginx.Listeners,
		}})
	if err != nil || verified.Nginx == nil || !verified.Nginx.ListenersMatched {
		return NginxRuntimeIdentityV2{}, errors.New("nginx_runtime_identity_unavailable")
	}
	binary := capabilities.Nginx.Binary.TargetPath
	if binary == "" {
		binary = capabilities.Nginx.Binary.Path
	}
	arguments := configureArgumentsFromHelperModulesV2(detected.NginxVersion.Modules)
	method := func(operation protectionhelper.Operation) NginxMethodCapabilityV2 {
		availability := CapabilityUnknownV2
		if protectionhelper.CapabilityAvailable(capabilities, operation) {
			availability = CapabilitySupportedV2
		}
		return NginxMethodCapabilityV2{Availability: availability, Revision: v2Revision(struct {
			Capability, Operation string
		}{capabilities.Revision, string(operation)})}
	}
	config := NginxRuntimeInspectionConfigV2{
		CandidatePaths: []string{binary}, AllowedExecutableRoots: []string{filepath.Dir(binary)},
		ManagedRootPath: capabilities.Nginx.ManagedRoot, ControlledConfigPath: capabilities.Nginx.ControlledConfig,
		InstallationClass: NginxInstallationManaged,
		ValidationMethod:  method(protectionhelper.OperationNginxValidate), ReloadMethod: method(protectionhelper.OperationNginxReload),
		ActiveVerification: method(protectionhelper.OperationNginxVerify), ProcessVerification: method(protectionhelper.OperationNginxVerify),
		ListenerVerification:          method(protectionhelper.OperationNginxVerify),
		ProxyProtocolReceive:          NginxMethodCapabilityV2{Availability: CapabilityUnknownV2},
		ProxyProtocolEmit:             NginxMethodCapabilityV2{Availability: CapabilityUnknownV2},
		MasterProcessIdentityRevision: v2Revision(struct{ PID int }{verified.Nginx.MasterPID}),
		WorkerSetIdentityRevision:     v2Revision(sortedIntsV2(verified.Nginx.WorkerPIDs)), ActiveManagedRevision: capabilities.Nginx.ActiveRevision,
		HelperProtocolVersion: protectionhelper.ProtocolVersion, HelperVersion: capabilities.HelperVersion,
		HelperContractVersion: capabilities.ContractVersion, HelperContractRevision: capabilities.Revision,
		ManagementExclusionsRevision: managementRevision, ObservedAt: now.UTC(), ExpiresAt: now.UTC().Add(time.Minute),
	}
	return (NginxRuntimeInspectorV2{Config: config, Reader: staticNginxVersionReaderV2{observation: NginxVersionObservationV2{
		Version: detected.NginxVersion.Version, ConfigureArguments: arguments,
	}}}).Inspect(ctx)
}

func configureArgumentsFromHelperModulesV2(modules []string) []string {
	seen := make(map[string]struct{})
	for _, module := range modules {
		argument := ""
		switch module {
		case "stream":
			argument = "--with-stream"
		case "ssl_preread":
			argument = "--with-stream_ssl_preread_module"
		case "stream_ssl", "ssl":
			argument = "--with-stream_ssl_module"
		case "stream_realip", "realip":
			argument = "--with-stream_realip_module"
		}
		if argument != "" {
			seen[argument] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for argument := range seen {
		result = append(result, argument)
	}
	sort.Strings(result)
	return result
}

func sortedIntsV2(values []int) []int {
	result := append([]int(nil), values...)
	sort.Ints(result)
	return result
}
