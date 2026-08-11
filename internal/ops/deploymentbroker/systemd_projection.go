package deploymentbroker

import (
	"sort"
	"strconv"
	"strings"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

const systemdObservationProperties = "LoadState,ActiveState,SubState,UnitFileState,NeedDaemonReload,User,Group,FragmentPath,DropInPaths,CapabilityBoundingSet,AmbientCapabilities,NoNewPrivileges,PrivateTmp,PrivateDevices,ProtectSystem,ProtectHome,ProtectKernelTunables,ProtectKernelModules,ProtectKernelLogs,ProtectControlGroups,ProtectClock,ProtectHostname,RestrictAddressFamilies,RestrictNamespaces,RestrictRealtime,RestrictSUIDSGID,LockPersonality,MemoryDenyWriteExecute,SystemCallArchitectures,UMask,ReadWritePaths,ReadOnlyPaths,RuntimeDirectory,StateDirectory,CacheDirectory,LogsDirectory,LimitNOFILE,TasksMax,MemoryHigh,MemoryMax,CPUQuotaPerSecUSec,TimeoutStartUSec,TimeoutStopUSec,Restart,RestartUSec,WatchdogUSec"

type systemdActualInput struct {
	Profile                     domain.ProfileID
	Properties                  map[string]string
	VersionOutput               string
	ManagerBootRevision         string
	DirectiveSupport            domain.Availability
	DirectiveCapabilityRevision string
	FragmentTrusted             bool
	FragmentRevision            string
	ExecutableRevision          string
	BrokerSocketRevision        string
	ObservedAt                  time.Time
}

func nativeCapabilities(posture *domain.Posture, now time.Time) domain.Capabilities {
	capabilities := domain.Capabilities{Observe: domain.Unavailable, Doctor: domain.Available, Migrate: domain.Unavailable, Rollback: domain.Available}
	if posture == nil {
		capabilities.Reasons = []string{"native_posture_unavailable"}
	} else {
		capabilities.Observe = domain.Available
		if posture.Validate(now) != nil {
			capabilities.Reasons = append(capabilities.Reasons, "native_posture_unverified")
		} else {
			capabilities.Migrate = domain.Available
		}
		if posture.Systemd == nil || posture.Systemd.DirectiveSupport != domain.Available {
			capabilities.Migrate = domain.Unavailable
			capabilities.Reasons = append(capabilities.Reasons, "systemd_directive_capability_unknown")
		}
	}
	capabilities.Reasons = uniqueCodes(capabilities.Reasons)
	capabilities.Revision = domain.Revision(capabilities)
	return capabilities
}

func projectSystemdActualState(input systemdActualInput) (domain.SystemdActualState, []string) {
	properties := input.Properties
	reasons := make([]string, 0, 8)
	version := parseSystemdVersion(input.VersionOutput)
	if version == "systemd-unknown" {
		reasons = append(reasons, "systemd_version_unknown")
	}
	bootRevision := verifiedRevision(input.ManagerBootRevision, "manager-boot-unavailable", &reasons, "systemd_manager_boot_identity_unavailable")
	directiveRevision := verifiedRevision(input.DirectiveCapabilityRevision, "directive-capability-unavailable", &reasons, "systemd_directive_capability_unknown")
	if input.DirectiveSupport != domain.Available {
		reasons = append(reasons, "systemd_directive_capability_unknown")
	}
	if !input.FragmentTrusted {
		reasons = append(reasons, "systemd_loaded_unit_identity_mismatch")
	}
	fragmentRevision := verifiedRevision(input.FragmentRevision, "fragment-unavailable", &reasons, "systemd_loaded_unit_identity_mismatch")
	dropIns := canonicalWords(properties["DropInPaths"])
	if len(dropIns) != 0 {
		reasons = append(reasons, "systemd_dropin_identity_mismatch")
	}
	executableRevision := verifiedRevision(input.ExecutableRevision, "executable-unavailable", &reasons, "panel_executable_identity_unsafe")
	brokerSocketRevision := verifiedRevision(input.BrokerSocketRevision, "broker-sockets-unavailable", &reasons, "broker_socket_identity_mismatch")
	now := input.ObservedAt.UTC().Truncate(time.Second)
	facts := domain.SystemdActualState{
		Schema: domain.SchemaV1, Version: version, ManagerBootRevision: bootRevision,
		DirectiveSupport: input.DirectiveSupport, DirectiveCapabilityRevision: directiveRevision,
		Unit: "solovey-ui.service", FragmentRevision: fragmentRevision, DropInRevision: domain.Revision(dropIns),
		UnitFileState: normalizedCode(properties["UnitFileState"]), LoadState: normalizedCode(properties["LoadState"]),
		ActiveState: normalizedCode(properties["ActiveState"]), SubState: normalizedCode(properties["SubState"]),
		DaemonReloadRequired: properties["NeedDaemonReload"] != "no",
		User:                 normalizedIdentity(properties["User"], "root"), Group: normalizedIdentity(properties["Group"], "root"),
		BoundingCapabilities: canonicalWords(properties["CapabilityBoundingSet"]), AmbientCapabilities: canonicalWords(properties["AmbientCapabilities"]),
		NoNewPrivileges: properties["NoNewPrivileges"] == "yes",
		SandboxRevision: domain.Revision(projectedProperties(properties, []string{"NoNewPrivileges", "PrivateTmp", "PrivateDevices", "ProtectSystem", "ProtectHome",
			"ProtectKernelTunables", "ProtectKernelModules", "ProtectKernelLogs", "ProtectControlGroups", "ProtectClock", "ProtectHostname",
			"RestrictAddressFamilies", "RestrictNamespaces", "RestrictRealtime", "RestrictSUIDSGID", "LockPersonality", "MemoryDenyWriteExecute", "SystemCallArchitectures", "UMask"})),
		WritePaths: canonicalPaths(properties["ReadWritePaths"]), ReadOnlyPaths: canonicalPaths(properties["ReadOnlyPaths"]),
		ExecutableRevision:       executableRevision,
		RuntimeDirectoryRevision: domain.Revision(projectedProperties(properties, []string{"RuntimeDirectory", "StateDirectory", "CacheDirectory", "LogsDirectory"})),
		ResourceRevision:         domain.Revision(projectedProperties(properties, []string{"LimitNOFILE", "TasksMax", "MemoryHigh", "MemoryMax", "CPUQuotaPerSecUSec", "TimeoutStartUSec", "TimeoutStopUSec"})),
		Restart:                  normalizedCode(properties["Restart"]), RestartUSec: normalizedCode(properties["RestartUSec"]), WatchdogUSec: normalizedCode(properties["WatchdogUSec"]),
		BrokerSocketRevision: brokerSocketRevision, ObservedAt: now.Unix(), ExpiresAt: now.Add(2 * time.Minute).Unix(),
	}
	if facts.UnitFileState != "enabled" {
		reasons = append(reasons, "systemd_unit_file_state_mismatch")
	}
	if facts.LoadState != "loaded" || facts.ActiveState != "active" || facts.SubState != "running" {
		reasons = append(reasons, "systemd_runtime_state_mismatch")
	}
	if facts.DaemonReloadRequired {
		reasons = append(reasons, "systemd_daemon_reload_required")
	}
	copy := facts
	copy.Revision = ""
	facts.Revision = domain.Revision(copy)
	return facts, uniqueCodes(reasons)
}

func systemdProfileReasons(profile domain.ProfileID, properties map[string]string, expectedWritePaths []string) []string {
	reasons := make([]string, 0, 8)
	if properties["LoadState"] != "loaded" || properties["ActiveState"] != "active" {
		reasons = append(reasons, "panel_service_not_active")
	}
	if properties["NeedDaemonReload"] != "no" {
		reasons = append(reasons, "systemd_daemon_reload_required")
	}
	if profile == domain.NativeLegacyRoot {
		if !oneOf(properties["User"], "", "root") || !oneOf(properties["Group"], "", "root") {
			reasons = append(reasons, "panel_identity_mismatch")
		}
		return append(reasons, resourceReasons(properties)...)
	}
	if properties["User"] != "solovey-ui" || properties["Group"] != "solovey-ui" {
		reasons = append(reasons, "panel_identity_mismatch")
	}
	exact := map[string]string{
		"NoNewPrivileges": "yes", "PrivateTmp": "yes", "PrivateDevices": "yes", "ProtectSystem": "strict", "ProtectHome": "yes",
		"ProtectKernelTunables": "yes", "ProtectKernelModules": "yes", "ProtectKernelLogs": "yes", "ProtectControlGroups": "yes",
		"ProtectClock": "yes", "ProtectHostname": "yes", "RestrictNamespaces": "yes", "RestrictRealtime": "yes",
		"RestrictSUIDSGID": "yes", "LockPersonality": "yes", "MemoryDenyWriteExecute": "yes", "SystemCallArchitectures": "native",
		"CapabilityBoundingSet": "", "AmbientCapabilities": "", "UMask": "0077",
	}
	for key, expected := range exact {
		if properties[key] != expected {
			reasons = append(reasons, "systemd_"+safePropertyCode(key)+"_mismatch")
		}
	}
	if !sameWords(properties["RestrictAddressFamilies"], []string{"AF_INET", "AF_INET6", "AF_UNIX"}) {
		reasons = append(reasons, "systemd_address_families_mismatch")
	}
	if len(expectedWritePaths) == 0 || !samePaths(properties["ReadWritePaths"], expectedWritePaths) {
		reasons = append(reasons, "systemd_write_paths_mismatch")
	}
	if !samePaths(properties["ReadOnlyPaths"], []string{"/etc/solovey-ui"}) {
		reasons = append(reasons, "systemd_readonly_paths_mismatch")
	}
	for key, expected := range map[string]string{"Restart": "on-failure", "RestartUSec": "5s", "WatchdogUSec": "0", "TimeoutStartUSec": "45s", "TimeoutStopUSec": "30s"} {
		if properties[key] != expected {
			reasons = append(reasons, "systemd_"+safePropertyCode(key)+"_mismatch")
		}
	}
	return append(reasons, resourceReasons(properties)...)
}

func parseSystemdVersion(output string) string {
	version, ok := systemdVersionNumber(output)
	if !ok {
		return "systemd-unknown"
	}
	return "systemd-" + strconv.FormatUint(version, 10)
}

func systemdVersionNumber(output string) (uint64, bool) {
	first := strings.SplitN(strings.TrimSpace(output), "\n", 2)[0]
	fields := strings.Fields(first)
	if len(fields) < 2 || fields[0] != "systemd" {
		return 0, false
	}
	value, err := strconv.ParseUint(fields[1], 10, 16)
	if err != nil || value == 0 {
		return 0, false
	}
	return value, true
}

func verifiedRevision(value, fallback string, reasons *[]string, reason string) string {
	if len(value) == 64 {
		valid := true
		for _, character := range value {
			if character < '0' || character > '9' && (character < 'a' || character > 'f') {
				valid = false
				break
			}
		}
		if valid {
			return value
		}
	}
	*reasons = append(*reasons, reason)
	return domain.Revision(fallback)
}

func normalizedCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("._-", character) {
			continue
		}
		return "unknown"
	}
	return value
}

func normalizedIdentity(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		value = fallback
	}
	return normalizedCode(value)
}

func canonicalWords(value string) []string {
	result := strings.Fields(value)
	sort.Strings(result)
	return compactStrings(result)
}

func canonicalPaths(value string) []string {
	result := strings.Fields(value)
	for index := range result {
		result[index] = strings.TrimLeft(result[index], "-+!")
	}
	sort.Strings(result)
	return compactStrings(result)
}

func compactStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func projectedProperties(properties map[string]string, keys []string) map[string]string {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		result[key] = properties[key]
	}
	return result
}

func uniqueCodes(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// unitPathDirective projects a bounded path allowlist from the root-owned,
// packaged unit that the broker already authenticated. This keeps component
// paths in the deployment artifact instead of duplicating component knowledge
// in core code.
func unitPathDirective(unit []byte, directive string) ([]string, bool) {
	if len(unit) == 0 || len(unit) > 128<<10 || directive == "" {
		return nil, false
	}
	section := ""
	var result []string
	for _, raw := range strings.Split(string(unit), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if section != "Service" || !strings.HasPrefix(line, directive+"=") {
			continue
		}
		if result != nil {
			return nil, false
		}
		result = strings.Fields(strings.TrimPrefix(line, directive+"="))
		if len(result) == 0 || len(result) > 32 {
			return nil, false
		}
		for _, value := range result {
			path := strings.TrimLeft(value, "-+!")
			if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "%*?[]{}") {
				return nil, false
			}
		}
	}
	return result, len(result) != 0
}

func resourceReasons(properties map[string]string) []string {
	reasons := make([]string, 0, 5)
	for key, expected := range map[string]string{"LimitNOFILE": "1048576", "TasksMax": "4096", "MemoryHigh": "805306368", "MemoryMax": "1073741824"} {
		if properties[key] != expected {
			reasons = append(reasons, "systemd_"+safePropertyCode(key)+"_mismatch")
		}
	}
	if !oneOf(properties["CPUQuotaPerSecUSec"], "2s", "2000000us") {
		reasons = append(reasons, "systemd_cpu_quota_mismatch")
	}
	return reasons
}

func sameWords(value string, expected []string) bool {
	actual := strings.Fields(value)
	sort.Strings(actual)
	wanted := append([]string(nil), expected...)
	sort.Strings(wanted)
	return strings.Join(actual, "\x00") == strings.Join(wanted, "\x00")
}

func samePaths(value string, expected []string) bool {
	actual := strings.Fields(value)
	for index := range actual {
		actual[index] = strings.TrimLeft(actual[index], "-+!")
	}
	sort.Strings(actual)
	wanted := append([]string(nil), expected...)
	sort.Strings(wanted)
	return strings.Join(actual, "\x00") == strings.Join(wanted, "\x00")
}

func oneOf(value string, expected ...string) bool {
	for _, candidate := range expected {
		if value == candidate {
			return true
		}
	}
	return false
}

func safePropertyCode(value string) string {
	var result strings.Builder
	for index, character := range value {
		if character >= 'A' && character <= 'Z' && index != 0 {
			result.WriteByte('_')
		}
		result.WriteRune(character)
	}
	return strings.ToLower(result.String())
}
