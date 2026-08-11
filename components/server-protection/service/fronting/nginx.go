package fronting

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const capabilityRevision = "fronting.nginx.v1"

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
	Platform          string // test seam; production defaults to runtime.GOOS.
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

type NginxAdapter struct {
	Config   NginxConfig
	Runner   ProbeRunner
	Workflow *Workflow
}

func NewNginxAdapter() *NginxAdapter { return &NginxAdapter{} }
func NewManagedNginxAdapter(workflow *Workflow) *NginxAdapter {
	return &NginxAdapter{Workflow: workflow}
}
func (a *NginxAdapter) ID() string   { return "nginx" }
func (a *NginxAdapter) Kind() string { return "stream" }

func (a *NginxAdapter) Capability(ctx context.Context) CapabilityReport {
	if a.Workflow != nil {
		return a.Workflow.Capability(ctx)
	}
	config := normalizedConfig(a.Config)
	report := baseReport(config)
	if config.Platform != "linux" {
		return unsupportedReport(report, StateUnsupported, "nginx stream detection requires Linux")
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
		return unsupportedReport(report, StateUnknown, "nginx capability probe requires the restricted helper")
	}
	result, err := runner.Run(probeCtx, path, []string{"-V"}, config.StdoutLimit, config.StderrLimit)
	if errors.Is(probeCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return unsupportedReport(report, StateTimeout, "nginx -V timed out")
	}
	if errors.Is(err, ErrOutputLimit) {
		return unsupportedReport(report, StateOversizedOutput, "nginx -V output exceeded a bounded limit")
	}
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, os.ErrPermission) {
		return unsupportedReport(report, StatePermissionDenied, "nginx binary cannot be executed")
	}
	if err != nil && result.ExitCode == 0 {
		return unsupportedReport(report, StateUnknown, "nginx probe failed without a safe result")
	}
	if result.ExitCode != 0 {
		return unsupportedReport(report, StateMalformedOutput, "nginx -V returned a non-zero exit code")
	}
	combined := append(append([]byte(nil), result.Stdout...), result.Stderr...)
	facts, parseErr := parseBuildFacts(combined)
	if parseErr != nil {
		return unsupportedReport(report, StateMalformedOutput, parseErr.Error())
	}
	report.Build = facts
	if config.ConfigRoot == "" && facts.ConfigPath != "" {
		config.ConfigRoot = filepath.Dir(facts.ConfigPath)
		report.Ownership.ConfigRoot = config.ConfigRoot
	}
	if facts.Version == "" {
		return unsupportedReport(report, StateVersionUnknown, "nginx version is not present in nginx -V output")
	}
	if !facts.StreamBuiltIn && !facts.StreamDynamicDeclared {
		return unsupportedReport(report, StateMissingStream, "stream module is not declared")
	}
	if !facts.SSLPrereadBuiltIn && !facts.SSLPrereadDynamicDeclared {
		return unsupportedReport(report, StateMissingSSLPreread, "ssl_preread module is not declared")
	}
	if (facts.StreamDynamicDeclared || facts.SSLPrereadDynamicDeclared) && !dynamicModulesReady(config, facts) {
		return unsupportedReport(report, StateDynamicModuleUnresolved, "a declared dynamic stream module is not confirmed available")
	}
	facts.DynamicModuleAvailable = facts.StreamDynamicDeclared || facts.SSLPrereadDynamicDeclared
	facts.ModuleRoots = append([]string(nil), config.ModuleRoots...)
	sort.Strings(facts.ModuleRoots)
	report.Build = facts
	if config.ExternalManaged {
		return unsupportedReport(report, StateExternalManaged, "nginx installation is marked external-managed")
	}
	if config.ConfigRoot == "" {
		return unsupportedReport(report, StateConfigRootUnknown, "nginx config root is not explicitly confirmed")
	}
	if config.ControlledInclude == "" {
		return unsupportedReport(report, StateIncludeNotControlled, "no Solovey-controlled include point is confirmed")
	}
	report.Supported = true
	report.State = StateSupported
	report.ManagedRoot = available(config.ManagedRoot != "")
	report.ControlledInclude = AvailabilitySupported
	report.SNI, report.ALPN, report.ProxyProtocol = AvailabilitySupported, AvailabilitySupported, AvailabilitySupported
	report.Validate, report.Activate, report.Reload = AvailabilityUnsupported, AvailabilityUnsupported, AvailabilityUnsupported
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
	if len(value.CandidatePaths) == 0 {
		value.CandidatePaths = []string{"/usr/sbin/nginx", "/usr/local/sbin/nginx", "/usr/bin/nginx", "/usr/local/bin/nginx"}
	}
	value.ConfigRoot = strings.TrimSpace(value.ConfigRoot)
	value.ManagedRoot = strings.TrimSpace(value.ManagedRoot)
	value.ControlledInclude = strings.TrimSpace(value.ControlledInclude)
	value.Platform = strings.ToLower(strings.TrimSpace(value.Platform))
	if value.Platform == "" {
		value.Platform = runtime.GOOS
	}
	return value
}

func baseReport(config NginxConfig) CapabilityReport {
	return CapabilityReport{AdapterID: "nginx", AdapterKind: "stream", CapabilityRevision: capabilityRevision, State: StateUnknown,
		Ownership:   ConfigOwnership{ConfigRoot: config.ConfigRoot, ManagedRoot: config.ManagedRoot, ControlledInclude: config.ControlledInclude, ExternalManaged: config.ExternalManaged},
		ManagedRoot: AvailabilityUnknown, ControlledInclude: AvailabilityUnknown, Validate: AvailabilityUnsupported, Activate: AvailabilityUnsupported, Reload: AvailabilityUnsupported,
		SNI: AvailabilityUnknown, ALPN: AvailabilityUnknown, ProxyProtocol: AvailabilityUnknown, Warnings: []string{}, Diagnostics: []string{}}
}

func unsupportedReport(report CapabilityReport, state DetectionState, reason string) CapabilityReport {
	report.State, report.Supported, report.Reason = state, false, reason
	report.ManagedRoot, report.ControlledInclude = AvailabilityUnsupported, AvailabilityUnsupported
	report.SNI, report.ALPN, report.ProxyProtocol = AvailabilityUnsupported, AvailabilityUnsupported, AvailabilityUnsupported
	report.Warnings = []string{reason, "preview-only: nginx validation, activation, reload, Sync, and Apply are unavailable"}
	return report
}

func available(ok bool) Availability {
	if ok {
		return AvailabilitySupported
	}
	return AvailabilityUnsupported
}

func resolveBinary(config NginxConfig) (string, BinaryIdentity, DetectionState, string) {
	candidates := config.CandidatePaths
	if config.BinaryOverride != "" {
		if !filepath.IsAbs(config.BinaryOverride) {
			return "", BinaryIdentity{}, StateUnsupported, "binary override must be an absolute path"
		}
		candidates = []string{config.BinaryOverride}
	}
	seen := map[string]BinaryIdentity{}
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
			return "", BinaryIdentity{}, StatePermissionDenied, "nginx binary cannot be inspected"
		}
		if err != nil {
			return "", BinaryIdentity{}, StateUnknown, "nginx binary identity is unavailable"
		}
		seen[identity.TargetPath] = identity
	}
	if len(seen) == 0 {
		return "", BinaryIdentity{}, StateNotFound, "no allowed nginx binary was found"
	}
	if len(seen) != 1 {
		return "", BinaryIdentity{}, StateMultipleBinaries, "multiple nginx binaries were found; an explicit typed override is required"
	}
	for path, identity := range seen {
		return path, identity, "", ""
	}
	panic("unreachable")
}

func fileIdentity(path string) (BinaryIdentity, error) {
	info, err := os.Stat(path) // Stat intentionally resolves symlinks before trust is assigned.
	if err != nil {
		return BinaryIdentity{}, err
	}
	if !info.Mode().IsRegular() {
		return BinaryIdentity{}, fmt.Errorf("nginx binary is not a regular file")
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return BinaryIdentity{}, err
	}
	identity := BinaryIdentity{Path: filepath.Clean(path), TargetPath: filepath.Clean(target)}
	// Windows has no syscall.Stat_t. This is the bounded platform-equivalent
	// identity there; Unix-specific device/inode can be added without changing
	// the public contract.
	identity.PlatformID = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
	return identity, nil
}

func parseBuildFacts(data []byte) (BuildFacts, error) {
	text := string(data)
	version := versionPattern.FindStringSubmatch(text)
	if len(version) != 2 {
		return BuildFacts{}, errors.New("nginx -V output is malformed")
	}
	facts := BuildFacts{Version: version[1], ConfigureArguments: []string{}}
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

func dynamicModulesReady(config NginxConfig, facts BuildFacts) bool {
	stream := !facts.StreamDynamicDeclared || config.DynamicModules["stream"]
	preread := !facts.SSLPrereadDynamicDeclared || config.DynamicModules["ssl_preread"]
	return stream && preread
}
