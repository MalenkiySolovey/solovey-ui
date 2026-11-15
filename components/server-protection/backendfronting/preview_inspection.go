package backendfronting

import (
	"context"
	"errors"
	"fmt"
	fronting "github.com/MalenkiySolovey/solovey-component-server-protection/service/fronting"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrOutputLimit = errors.New("probe output limit exceeded")
	versionPattern = regexp.MustCompile(`(?m)nginx/([0-9][0-9A-Za-z.+~-]*)`)
)

type NginxConfig struct {
	// BinaryOverride is an advanced typed setting. It must be an absolute file.
	BinaryOverride    string
	CandidatePaths    []string
	ConfigRoot        string
	ManagedRoot       string
	ControlledInclude string
	ExternalManaged   bool
	DynamicModules    map[string]bool
	ModuleRoots       []string
	ProbeCapability   fronting.Availability
	Timeout           time.Duration
	StdoutLimit       int
	StderrLimit       int
}

type ProbeResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type ProbeRunner interface {
	Run(context.Context, string, []string, int, int) (ProbeResult, error)
}

type PreviewInspector struct {
	Config NginxConfig
	Runner ProbeRunner
}

func (a *PreviewInspector) Capability(ctx context.Context) fronting.CapabilityReport {
	config := normalizedConfig(a.Config)
	report := baseReport(config)
	if config.ProbeCapability != fronting.AvailabilitySupported {
		return unsupportedReport(report, fronting.StateUnsupported, "nginx binary inspection capability is unavailable")
	}
	path, identity, state, reason := resolveBinary(config)
	if state != "" {
		return unsupportedReport(report, state, reason)
	}
	report.Binary = identity
	probeCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	runner := a.Runner
	if runner == nil {
		return unsupportedReport(report, fronting.StateUnknown, "nginx capability probe requires the restricted helper")
	}
	result, err := runner.Run(probeCtx, path, []string{"-V"}, config.StdoutLimit, config.StderrLimit)
	if errors.Is(probeCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return unsupportedReport(report, fronting.StateTimeout, "nginx -V timed out")
	}
	if errors.Is(err, ErrOutputLimit) {
		return unsupportedReport(report, fronting.StateOversizedOutput, "nginx -V output exceeded a bounded limit")
	}
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, os.ErrPermission) {
		return unsupportedReport(report, fronting.StatePermissionDenied, "nginx binary cannot be executed")
	}
	if err != nil && result.ExitCode == 0 {
		return unsupportedReport(report, fronting.StateUnknown, "nginx probe failed without a safe result")
	}
	if result.ExitCode != 0 {
		return unsupportedReport(report, fronting.StateMalformedOutput, "nginx -V returned a non-zero exit code")
	}
	combined := append(append([]byte(nil), result.Stdout...), result.Stderr...)
	facts, parseErr := parseBuildFacts(combined)
	if parseErr != nil {
		return unsupportedReport(report, fronting.StateMalformedOutput, parseErr.Error())
	}
	report.Build = facts
	if config.ConfigRoot == "" && facts.ConfigPath != "" {
		config.ConfigRoot = filepath.Dir(facts.ConfigPath)
		report.Ownership.ConfigRoot = config.ConfigRoot
	}
	if facts.Version == "" {
		return unsupportedReport(report, fronting.StateVersionUnknown, "nginx version is not present in nginx -V output")
	}
	if !facts.StreamBuiltIn && !facts.StreamDynamicDeclared {
		return unsupportedReport(report, fronting.StateMissingStream, "stream module is not declared")
	}
	if !facts.SSLPrereadBuiltIn && !facts.SSLPrereadDynamicDeclared {
		return unsupportedReport(report, fronting.StateMissingSSLPreread, "ssl_preread module is not declared")
	}
	if (facts.StreamDynamicDeclared || facts.SSLPrereadDynamicDeclared) && !dynamicModulesReady(config, facts) {
		return unsupportedReport(report, fronting.StateDynamicModuleUnresolved, "a declared dynamic stream module is not confirmed available")
	}
	facts.DynamicModuleAvailable = facts.StreamDynamicDeclared || facts.SSLPrereadDynamicDeclared
	facts.ModuleRoots = append([]string(nil), config.ModuleRoots...)
	sort.Strings(facts.ModuleRoots)
	report.Build = facts
	if config.ExternalManaged {
		return unsupportedReport(report, fronting.StateExternalManaged, "nginx installation is marked external-managed")
	}
	if config.ConfigRoot == "" {
		return unsupportedReport(report, fronting.StateConfigRootUnknown, "nginx config root is not explicitly confirmed")
	}
	if config.ControlledInclude == "" {
		return unsupportedReport(report, fronting.StateIncludeNotControlled, "no Solovey-controlled include point is confirmed")
	}
	report.Supported = true
	report.State = fronting.StateSupported
	report.ManagedRoot = available(config.ManagedRoot != "")
	report.ControlledInclude = fronting.AvailabilitySupported
	report.SNI, report.ALPN, report.ProxyProtocol = fronting.AvailabilitySupported, fronting.AvailabilitySupported, fronting.AvailabilitySupported
	report.Validate, report.Activate, report.Reload = fronting.AvailabilityUnsupported, fronting.AvailabilityUnsupported, fronting.AvailabilityUnsupported
	report.Warnings = []string{"preview-only: nginx validation, activation, reload, Sync, and Apply are unavailable"}
	report.Diagnostics = []string{"nginx -V probe completed with bounded stdout/stderr", "no nginx configuration dump or filesystem write was used"}
	return report
}

func normalizedConfig(value NginxConfig) NginxConfig {
	if value.Timeout <= 0 {
		value.Timeout = 3 * time.Second
	}
	if value.StdoutLimit <= 0 {
		value.StdoutLimit = 32 * 1024
	}
	if value.StderrLimit <= 0 {
		value.StderrLimit = 64 * 1024
	}
	// Candidate discovery belongs only to the explicit preview/config seam.
	// Production composition leaves ProbeCapability unsupported and consumes
	// the restricted helper's injected fronting.BinaryIdentity through Workflow.
	if value.ProbeCapability == fronting.AvailabilitySupported && len(value.CandidatePaths) == 0 {
		value.CandidatePaths = []string{"/usr/sbin/nginx", "/usr/local/sbin/nginx", "/usr/bin/nginx", "/usr/local/bin/nginx"}
	}
	value.ConfigRoot = strings.TrimSpace(value.ConfigRoot)
	value.ManagedRoot = strings.TrimSpace(value.ManagedRoot)
	value.ControlledInclude = strings.TrimSpace(value.ControlledInclude)
	return value
}

func baseReport(config NginxConfig) fronting.CapabilityReport {
	return fronting.CapabilityReport{AdapterID: "nginx", AdapterKind: "stream", CapabilityRevision: fronting.NginxCapabilityRevision, State: fronting.StateUnknown,
		Ownership:   fronting.ConfigOwnership{ConfigRoot: config.ConfigRoot, ManagedRoot: config.ManagedRoot, ControlledInclude: config.ControlledInclude, ExternalManaged: config.ExternalManaged},
		ManagedRoot: fronting.AvailabilityUnknown, ControlledInclude: fronting.AvailabilityUnknown, Validate: fronting.AvailabilityUnsupported, Activate: fronting.AvailabilityUnsupported, Reload: fronting.AvailabilityUnsupported,
		SNI: fronting.AvailabilityUnknown, ALPN: fronting.AvailabilityUnknown, ProxyProtocol: fronting.AvailabilityUnknown, Warnings: []string{}, Diagnostics: []string{}}
}

func unsupportedReport(report fronting.CapabilityReport, state fronting.DetectionState, reason string) fronting.CapabilityReport {
	report.State, report.Supported, report.Reason = state, false, reason
	report.ManagedRoot, report.ControlledInclude = fronting.AvailabilityUnsupported, fronting.AvailabilityUnsupported
	report.SNI, report.ALPN, report.ProxyProtocol = fronting.AvailabilityUnsupported, fronting.AvailabilityUnsupported, fronting.AvailabilityUnsupported
	report.Warnings = []string{reason, "preview-only: nginx validation, activation, reload, Sync, and Apply are unavailable"}
	return report
}

func available(ok bool) fronting.Availability {
	if ok {
		return fronting.AvailabilitySupported
	}
	return fronting.AvailabilityUnsupported
}

func resolveBinary(config NginxConfig) (string, fronting.BinaryIdentity, fronting.DetectionState, string) {
	candidates := config.CandidatePaths
	if config.BinaryOverride != "" {
		if !filepath.IsAbs(config.BinaryOverride) {
			return "", fronting.BinaryIdentity{}, fronting.StateUnsupported, "binary override must be an absolute path"
		}
		candidates = []string{config.BinaryOverride}
	}
	seen := map[string]fronting.BinaryIdentity{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(strings.TrimSpace(candidate))
		if candidate == "." || !filepath.IsAbs(candidate) {
			continue
		}
		identity, err := fileIdentity(candidate)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if errors.Is(err, fs.ErrPermission) || errors.Is(err, os.ErrPermission) {
			return "", fronting.BinaryIdentity{}, fronting.StatePermissionDenied, "nginx binary cannot be inspected"
		}
		if err != nil {
			return "", fronting.BinaryIdentity{}, fronting.StateUnknown, "nginx binary identity is unavailable"
		}
		seen[identity.TargetPath] = identity
	}
	if len(seen) == 0 {
		return "", fronting.BinaryIdentity{}, fronting.StateNotFound, "no allowed nginx binary was found"
	}
	if len(seen) != 1 {
		return "", fronting.BinaryIdentity{}, fronting.StateMultipleBinaries, "multiple nginx binaries were found; an explicit typed override is required"
	}
	for path, identity := range seen {
		return path, identity, "", ""
	}
	panic("unreachable")
}

func fileIdentity(path string) (fronting.BinaryIdentity, error) {
	info, err := os.Stat(path) // Stat intentionally resolves symlinks before trust is assigned.
	if err != nil {
		return fronting.BinaryIdentity{}, err
	}
	if !info.Mode().IsRegular() {
		return fronting.BinaryIdentity{}, fmt.Errorf("nginx binary is not a regular file")
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fronting.BinaryIdentity{}, err
	}
	identity := fronting.BinaryIdentity{Path: filepath.Clean(path), TargetPath: filepath.Clean(target)}
	// Windows has no syscall.Stat_t. This is the bounded platform-equivalent
	// identity there; Unix-specific device/inode can be added without changing
	// the public contract.
	identity.PlatformID = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
	return identity, nil
}

func parseBuildFacts(data []byte) (fronting.BuildFacts, error) {
	text := string(data)
	version := versionPattern.FindStringSubmatch(text)
	if len(version) != 2 {
		return fronting.BuildFacts{}, errors.New("nginx -V output is malformed")
	}
	facts := fronting.BuildFacts{Version: version[1], ConfigureArguments: []string{}}
	index := strings.Index(text, "configure arguments:")
	if index < 0 {
		return facts, nil
	}
	arguments := strings.Fields(text[index+len("configure arguments:"):])
	sort.Strings(arguments)
	facts.ConfigureArguments = arguments
	for _, argument := range arguments {
		switch argument {
		case "--with-stream":
			facts.StreamBuiltIn = true
		case "--with-stream_ssl_preread_module":
			facts.SSLPrereadBuiltIn = true
		case "--with-stream=dynamic":
			facts.StreamDynamicDeclared = true
		case "--with-stream_ssl_preread_module=dynamic":
			facts.SSLPrereadDynamicDeclared = true
		}
		if strings.HasPrefix(argument, "--conf-path=") {
			candidate := strings.TrimPrefix(argument, "--conf-path=")
			if filepath.IsAbs(candidate) {
				facts.ConfigPath = filepath.Clean(candidate)
			}
		}
	}
	return facts, nil
}

func dynamicModulesReady(config NginxConfig, facts fronting.BuildFacts) bool {
	stream := !facts.StreamDynamicDeclared || config.DynamicModules["stream"]
	preread := !facts.SSLPrereadDynamicDeclared || config.DynamicModules["ssl_preread"]
	return stream && preread
}
