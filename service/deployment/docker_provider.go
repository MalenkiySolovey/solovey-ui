package deployment

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

type DockerProvider struct{ now func() time.Time }

type dockerRuntimeFacts struct {
	NoNewPrivileges       bool     `json:"noNewPrivileges"`
	EffectiveCapabilities []string `json:"effectiveCapabilities"`
	RootReadOnly          bool     `json:"rootReadOnly"`
	DataWritable          bool     `json:"dataWritable"`
	CertificateWritable   bool     `json:"certificateWritable"`
	TemporaryTmpfs        bool     `json:"temporaryTmpfs"`
	RuntimeTmpfs          bool     `json:"runtimeTmpfs"`
	TUNDevice             bool     `json:"tunDevice"`
	UserNamespace         string   `json:"userNamespace"`
	NetworkMode           string   `json:"networkMode"`
	EngineMode            string   `json:"engineMode"`
}

func NewDockerProvider() *DockerProvider { return &DockerProvider{now: time.Now} }

func (*DockerProvider) ProviderID() string { return "local-docker-posture/v1" }

func (*DockerProvider) Capabilities(context.Context) domain.Capabilities {
	result := domain.Capabilities{Observe: domain.Available, Doctor: domain.Available, Migrate: domain.Unavailable,
		Rollback: domain.Unavailable, Reasons: []string{"docker_profile_changes_require_operator_recreate"}}
	result.Revision = domain.Revision(result)
	return result
}

func (p *DockerProvider) Observe(context.Context) (domain.Posture, error) {
	profileID := domain.ProfileID(strings.TrimSpace(os.Getenv("SOLOVEY_DEPLOYMENT_PROFILE")))
	profile, ok := domain.Lookup(profileID)
	if !ok || profile.Runtime != domain.RuntimeDocker || !DetectedDocker() {
		return domain.Posture{}, ErrProviderUnavailable
	}
	executable, err := os.Executable()
	if err != nil {
		return domain.Posture{}, err
	}
	info, err := os.Stat(executable)
	if err != nil {
		return domain.Posture{}, err
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil && runtime.GOOS == "linux" {
		return domain.Posture{}, err
	}
	mountInfo, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil && runtime.GOOS == "linux" {
		return domain.Posture{}, err
	}
	uidMap, _ := os.ReadFile("/proc/self/uid_map")
	_, tunErr := os.Stat("/dev/net/tun")
	facts, reasons := projectDockerRuntime(profileID, os.Geteuid(), os.Getegid(), status, mountInfo, uidMap, tunErr == nil)
	dataRevision, err := localDirectoryRevision(os.Getenv("SUI_DB_FOLDER"))
	if err != nil {
		return domain.Posture{}, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	if p != nil && p.now != nil {
		now = p.now().UTC().Truncate(time.Second)
	}
	posture := domain.Posture{Schema: domain.SchemaV1, Profile: profileID, InstalledProfile: profileID, ActiveProfile: profileID, Runtime: domain.RuntimeDocker,
		PanelUID: uint32(maxInt(os.Geteuid(), 0)), PanelGID: uint32(maxInt(os.Getegid(), 0)), PanelRoot: os.Geteuid() == 0,
		ServiceRevision: domain.Revision(struct {
			Path string
			Size int64
			Mode uint32
		}{filepath.Base(executable), info.Size(), uint32(info.Mode().Perm())}), DataRevision: dataRevision,
		HardeningRevision: domain.Revision(facts), ObservedAt: now.Unix(), ExpiresAt: now.Add(2 * time.Minute).Unix(), Reasons: reasons}
	// The panel intentionally has no docker.sock or daemon API. It can prove
	// in-container hardening, but cannot attest daemon mode or host/bridge
	// topology. Those remain explicit unknown facts until an external Live
	// inspector supplies evidence; normal CI must not claim them verified.
	if len(reasons) == 0 {
		posture.VerifiedProfile = profileID
	}
	domain.SetPostureRevision(&posture)
	return posture, nil
}

func (p *DockerProvider) Doctor(ctx context.Context) (domain.DoctorReport, error) {
	posture, err := p.Observe(ctx)
	capabilities := p.Capabilities(ctx)
	report := domain.DoctorReport{Capabilities: capabilities, GeneratedAt: time.Now().UTC().Unix()}
	if err != nil {
		report.Findings = append(report.Findings, domain.Finding{Code: "docker_posture_unavailable", Severity: domain.SeverityCritical,
			MessageKey: "deployment.doctor.dockerPostureUnavailable", Remediation: "set the packaged SOLOVEY_DEPLOYMENT_PROFILE value and recreate the container"})
		return domain.FinalizeDoctor(report), nil
	}
	report.Posture = &posture
	for _, reason := range posture.Reasons {
		severity := domain.SeverityCritical
		remediation := "restore the packaged container profile and recreate the container"
		if reason == "docker_network_mode_unattested" || reason == "docker_engine_mode_unattested" {
			severity = domain.SeverityWarning
			remediation = "verify this fact from the operator-controlled Docker engine; the panel has no daemon control channel"
		}
		report.Findings = append(report.Findings, domain.Finding{Code: reason, Severity: severity,
			MessageKey: "deployment.doctor." + reason, Remediation: remediation})
	}
	if posture.Profile == domain.DockerNetworkAdvanced {
		report.Findings = append(report.Findings, domain.Finding{Code: "experimental_profile", Severity: domain.SeverityWarning,
			MessageKey: "deployment.doctor.experimentalProfile", Remediation: "keep host-level recovery access before recreating the container"})
	}
	return domain.FinalizeDoctor(report), nil
}

func (*DockerProvider) Prepare(context.Context, FenceV1, domain.ProfileID) (string, error) {
	return "", ErrProviderUnavailable
}
func (*DockerProvider) Apply(context.Context, FenceV1, domain.ProfileID, string) error {
	return ErrProviderUnavailable
}
func (*DockerProvider) Verify(context.Context, FenceV1, domain.ProfileID, string) (domain.Posture, error) {
	return domain.Posture{}, ErrProviderUnavailable
}
func (*DockerProvider) Rollback(context.Context, FenceV1, domain.ProfileID, string) (domain.Posture, error) {
	return domain.Posture{}, ErrProviderUnavailable
}

func DetectedDocker() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	for _, path := range []string{"/.dockerenv", "/run/.containerenv"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return true
		}
	}
	data, _ := os.ReadFile("/proc/1/cgroup")
	text := strings.ToLower(string(data))
	return strings.Contains(text, "docker") || strings.Contains(text, "containerd") || strings.Contains(text, "podman")
}

func RuntimeProvider() Provider {
	if DetectedDocker() {
		return NewDockerProvider()
	}
	return NewBrokerProvider(nil)
}

func parseProcStatus(data []byte) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), ":", 2)
		if len(parts) == 2 {
			result[parts[0]] = strings.TrimSpace(parts[1])
		}
	}
	return result
}

func projectDockerRuntime(profile domain.ProfileID, uid, gid int, status, mountInfo, uidMap []byte, tun bool) (dockerRuntimeFacts, []string) {
	properties := parseProcStatus(status)
	capabilities := effectiveCapabilities(properties["CapEff"])
	mounts := parseMountInfo(mountInfo)
	facts := dockerRuntimeFacts{NoNewPrivileges: properties["NoNewPrivs"] == "1", EffectiveCapabilities: capabilities,
		RootReadOnly: mountHas(mounts, "/", "ro", ""), DataWritable: mountHas(mounts, "/data", "rw", ""),
		CertificateWritable: mountHas(mounts, "/cert", "rw", ""), TemporaryTmpfs: mountHas(mounts, "/tmp", "rw", "tmpfs"),
		RuntimeTmpfs: mountHas(mounts, "/run/solovey-ui", "rw", "tmpfs"),
		TUNDevice:    tun, UserNamespace: userNamespaceMode(uidMap), NetworkMode: "unattested", EngineMode: "unattested"}
	reasons := make([]string, 0, 12)
	if uid != 65532 || gid != 65532 {
		reasons = append(reasons, "docker_process_identity_mismatch")
	}
	if !facts.NoNewPrivileges {
		reasons = append(reasons, "docker_no_new_privileges_missing")
	}
	expectedCapabilities := []string{}
	if profile == domain.DockerNetworkAdvanced {
		expectedCapabilities = []string{"NET_ADMIN", "NET_BIND_SERVICE", "NET_RAW"}
	}
	if strings.Join(capabilities, ",") != strings.Join(expectedCapabilities, ",") {
		reasons = append(reasons, "docker_capability_set_mismatch")
	}
	if !facts.RootReadOnly {
		reasons = append(reasons, "docker_root_filesystem_writable")
	}
	if !facts.DataWritable || !facts.CertificateWritable {
		reasons = append(reasons, "docker_bound_write_scope_mismatch")
	}
	if !facts.TemporaryTmpfs || !facts.RuntimeTmpfs {
		reasons = append(reasons, "docker_tmpfs_mismatch")
	}
	if profile == domain.DockerNetworkAdvanced && !tun {
		reasons = append(reasons, "docker_tun_device_missing")
	}
	if facts.UserNamespace == "unknown" {
		reasons = append(reasons, "docker_user_namespace_unattested")
	}
	reasons = append(reasons, "docker_network_mode_unattested", "docker_engine_mode_unattested")
	return facts, reasons
}

type mountFact struct {
	Options map[string]bool
	FSType  string
}

func parseMountInfo(data []byte) map[string]mountFact {
	result := make(map[string]mountFact)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if len(fields) < 6 || separator < 0 || separator+1 >= len(fields) {
			continue
		}
		path := decodeMountField(fields[4])
		options := make(map[string]bool)
		for _, option := range strings.Split(fields[5], ",") {
			options[option] = true
		}
		result[path] = mountFact{Options: options, FSType: fields[separator+1]}
	}
	return result
}

func decodeMountField(value string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(value)
}

func mountHas(mounts map[string]mountFact, path, option, fsType string) bool {
	fact, ok := mounts[path]
	return ok && fact.Options[option] && (fsType == "" || fact.FSType == fsType)
}

func effectiveCapabilities(value string) []string {
	bits, err := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
	if err != nil {
		return []string{"UNKNOWN"}
	}
	names := map[uint]string{10: "NET_BIND_SERVICE", 12: "NET_ADMIN", 13: "NET_RAW"}
	result := make([]string, 0, 4)
	for bit := uint(0); bit < 64; bit++ {
		if bits&(uint64(1)<<bit) == 0 {
			continue
		}
		if name, ok := names[bit]; ok {
			result = append(result, name)
		} else {
			result = append(result, "EXCESS_"+strconv.FormatUint(uint64(bit), 10))
		}
	}
	sort.Strings(result)
	return result
}

func userNamespaceMode(data []byte) string {
	fields := strings.Fields(string(data))
	if len(fields) != 3 || fields[0] != "0" {
		return "unknown"
	}
	length, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return "unknown"
	}
	if fields[1] == "0" && length >= 4294967295 {
		return "initial"
	}
	return "remapped"
}

func localDirectoryRevision(path string) (string, error) {
	path = filepath.Clean(path)
	if path == "." || !filepath.IsAbs(path) {
		return "", errors.New("container data directory is not explicit")
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) > 256 {
		return "", errors.New("container data directory is unavailable")
	}
	type fact struct {
		Name string
		Size int64
		Mode uint32
	}
	facts := make([]fact, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return "", errors.New("container data entry is unsafe")
		}
		facts = append(facts, fact{entry.Name(), info.Size(), uint32(info.Mode().Perm())})
	}
	return domain.Revision(facts), nil
}

func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
