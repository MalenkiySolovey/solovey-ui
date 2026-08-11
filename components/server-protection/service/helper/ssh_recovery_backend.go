package helper

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxSSHVerifierFiles = 512
	maxSSHVerifierBytes = 2 << 20
)

var acceptedSSHLogin = regexp.MustCompile(`^Accepted (publickey|password|keyboard-interactive) for ([A-Za-z0-9._@-]{1,128}) from ([^ ]{1,128}) port ([0-9]{1,5}) ssh2(?:[: ].*)?$`)

type SSHRecoveryExecutor interface {
	Detect(context.Context) SSHRecoverySupport
	Observe(context.Context, SSHRecoveryObserveRequest) (*SSHRecoveryResult, error)
}

type systemSSHRecoveryExecutor struct {
	journalctl string
	sshd       string
}

func newSystemSSHRecoveryExecutor() SSHRecoveryExecutor {
	if runtime.GOOS != "linux" {
		return systemSSHRecoveryExecutor{}
	}
	return systemSSHRecoveryExecutor{
		journalctl: firstRegularExecutable("/usr/bin/journalctl", "/bin/journalctl"),
		sshd:       firstRegularExecutable("/usr/sbin/sshd", "/usr/bin/sshd", "/sbin/sshd"),
	}
}

func firstRegularExecutable(values ...string) string {
	for _, value := range values {
		if info, err := os.Lstat(value); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			return value
		}
	}
	return ""
}

func (e systemSSHRecoveryExecutor) Detect(ctx context.Context) SSHRecoverySupport {
	if runtime.GOOS != "linux" {
		return SSHRecoverySupport{PlatformKnown: true, Linux: false, Reason: "linux_required"}
	}
	if e.journalctl == "" || e.sshd == "" {
		return SSHRecoverySupport{PlatformKnown: true, Linux: true, Reason: "ssh_audit_dependencies_missing"}
	}
	revision, err := e.verifierRevision(ctx)
	if err != nil {
		return SSHRecoverySupport{PlatformKnown: true, Linux: true, JournalBinary: e.journalctl, Reason: "ssh_verifier_revision_unproven"}
	}
	return SSHRecoverySupport{PlatformKnown: true, Linux: true, Available: true, JournalBinary: e.journalctl, VerifierRevision: revision}
}

func (e systemSSHRecoveryExecutor) Observe(ctx context.Context, request SSHRecoveryObserveRequest) (*SSHRecoveryResult, error) {
	support := e.Detect(ctx)
	if !support.Available {
		return nil, errors.New(support.Reason)
	}
	since := fmt.Sprintf("@%d.%06d", request.SinceUnixMicros/1_000_000, request.SinceUnixMicros%1_000_000)
	stdout, stderr, err := runSSHRecoveryBounded(ctx, e.journalctl, []string{"--unit=ssh.service", "--unit=sshd.service", "--since=" + since, "--output=json", "--no-pager", "--lines=512"})
	if err != nil {
		return nil, fmt.Errorf("SSH audit journal query failed: %w (%s)", err, boundedSSHDiagnostic(stderr))
	}
	observations, err := parseSSHRecoveryJournal(stdout, request, time.Now().UTC(), e.sshd)
	if err != nil {
		return nil, err
	}
	return &SSHRecoveryResult{VerifierRevision: support.VerifierRevision, Observations: observations}, nil
}

func (e systemSSHRecoveryExecutor) verifierRevision(ctx context.Context) (string, error) {
	stdout, stderr, err := runSSHRecoveryBounded(ctx, e.sshd, []string{"-T"})
	if err != nil {
		return "", fmt.Errorf("effective sshd configuration is unavailable: %w (%s)", err, boundedSSHDiagnostic(stderr))
	}
	effective := strings.ToLower(string(stdout))
	if !sshPublicKeyVerifierConfigurationProven(effective) {
		return "", errors.New("effective SSH public-key verification is not locally provable")
	}
	paths := []string{"/etc/ssh/sshd_config", "/etc/passwd", e.journalctl, e.sshd}
	for _, pattern := range []string{"/etc/ssh/sshd_config.d/*.conf", "/etc/ssh/ssh_host_*_key.pub"} {
		matches, globErr := filepath.Glob(pattern)
		if globErr != nil {
			return "", globErr
		}
		paths = append(paths, matches...)
	}
	passwd, _, err := readSSHVerifierFile("/etc/passwd", maxSSHVerifierBytes)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(passwd), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 7 || fields[5] == "" || !filepath.IsAbs(fields[5]) || filepath.Clean(fields[5]) != fields[5] {
			continue
		}
		for _, name := range []string{"authorized_keys", "authorized_keys2"} {
			candidate := filepath.Join(fields[5], ".ssh", name)
			if _, statErr := os.Lstat(candidate); statErr == nil {
				paths = append(paths, candidate)
			}
		}
	}
	paths = uniqueSSHPaths(paths)
	if len(paths) == 0 || len(paths) > maxSSHVerifierFiles {
		return "", errors.New("SSH verifier file inventory is empty or exceeds its bound")
	}
	type fileRevision struct {
		Path   string `json:"path"`
		Mode   uint32 `json:"mode"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	}
	files := make([]fileRevision, 0, len(paths))
	total := 0
	for _, path := range paths {
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			return "", errors.New("SSH verifier contains a missing, non-regular, or linked file")
		}
		remaining := maxSSHVerifierBytes - total
		if remaining <= 0 || info.Size() > int64(remaining) {
			return "", errors.New("SSH verifier files exceed the bounded byte budget")
		}
		data, exactInfo, readErr := readSSHVerifierFile(path, remaining)
		if readErr != nil {
			return "", readErr
		}
		total += len(data)
		files = append(files, fileRevision{Path: path, Mode: uint32(exactInfo.Mode().Perm()), Size: exactInfo.Size(), SHA256: sha256Hex(data)})
	}
	payload, _ := json.Marshal(struct {
		Contract  string         `json:"contract"`
		Effective string         `json:"effective_sha256"`
		Files     []fileRevision `json:"files"`
	}{"ssh-recovery-verifier/v1", sha256Hex(stdout), files})
	return sha256Hex(payload), nil
}

func sshPublicKeyVerifierConfigurationProven(effective string) bool {
	effective = strings.ToLower(effective)
	return strings.Contains(effective, "pubkeyauthentication yes\n") &&
		strings.Contains(effective, "authorizedkeyscommand none\n") &&
		strings.Contains(effective, "strictmodes yes\n")
}

func uniqueSSHPaths(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.Clean(value)
		if !filepath.IsAbs(value) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func readSSHVerifierFile(path string, limit int) ([]byte, os.FileInfo, error) {
	if limit < 1 {
		return nil, nil, io.ErrShortBuffer
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return nil, nil, errors.New("SSH verifier contains a missing, non-regular, or linked file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, nil, errors.New("SSH verifier file identity changed before read")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > limit {
		return nil, nil, io.ErrShortBuffer
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, after) || opened.Size() != int64(len(data)) {
		return nil, nil, errors.New("SSH verifier file identity changed during read")
	}
	return data, opened, nil
}

func parseSSHRecoveryJournal(data []byte, request SSHRecoveryObserveRequest, now time.Time, expectedExecutable string) ([]SSHRecoveryObservation, error) {
	result := make([]SSHRecoveryObservation, 0)
	seen := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 64<<10), MaxOutputBytes)
	for scanner.Scan() {
		var row struct {
			Message    string `json:"MESSAGE"`
			Unit       string `json:"_SYSTEMD_UNIT"`
			Identifier string `json:"SYSLOG_IDENTIFIER"`
			Executable string `json:"_EXE"`
			Realtime   string `json:"__REALTIME_TIMESTAMP"`
			Cursor     string `json:"__CURSOR"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, errors.New("SSH audit journal returned malformed JSON")
		}
		if row.Identifier != "sshd" || row.Executable != expectedExecutable || row.Unit != "ssh.service" && row.Unit != "sshd.service" {
			continue
		}
		match := acceptedSSHLogin.FindStringSubmatch(row.Message)
		if match == nil || match[1] != "publickey" {
			continue
		}
		address, err := netip.ParseAddr(match[3])
		if err != nil || address.IsUnspecified() || address.IsMulticast() {
			continue
		}
		if _, err := strconv.ParseUint(match[4], 10, 16); err != nil {
			continue
		}
		address = address.Unmap()
		micros, err := strconv.ParseInt(row.Realtime, 10, 64)
		if err != nil || micros <= request.SinceUnixMicros || micros > now.Add(5*time.Minute).UnixMicro() {
			continue
		}
		bits := 128
		if address.Is4() {
			bits = 32
		}
		principalSum := sha256.Sum256([]byte("ssh:" + match[2]))
		observationSum := sha256.Sum256([]byte(row.Cursor + "\x00" + row.Realtime + "\x00" + match[1] + "\x00" + match[2] + "\x00" + address.String()))
		observation := SSHRecoveryObservation{
			ObservationID:       "recovery:" + hex.EncodeToString(observationSum[:]),
			PrincipalID:         "principal:" + hex.EncodeToString(principalSum[:]),
			SourcePrefix:        netip.PrefixFrom(address, bits).String(),
			AuthenticationClass: match[1], ObservedAt: micros / 1_000_000, ObservedAtMicros: micros,
		}
		if _, exists := seen[observation.ObservationID]; exists {
			continue
		}
		seen[observation.ObservationID] = struct{}{}
		result = append(result, observation)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ObservedAtMicros != result[j].ObservedAtMicros {
			return result[i].ObservedAtMicros < result[j].ObservedAtMicros
		}
		return result[i].ObservationID < result[j].ObservationID
	})
	if len(result) > request.MaxEvents {
		result = result[len(result)-request.MaxEvents:]
	}
	return result, nil
}

func runSSHRecoveryBounded(ctx context.Context, binary string, args []string) ([]byte, []byte, error) {
	stdout, stderr := &boundedBuffer{limit: MaxOutputBytes}, &boundedBuffer{limit: MaxOutputBytes}
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()
	if stdout.truncated || stderr.truncated {
		return stdout.buffer.Bytes(), stderr.buffer.Bytes(), errors.New("SSH recovery command output exceeded its bound")
	}
	return stdout.buffer.Bytes(), stderr.buffer.Bytes(), err
}

func boundedSSHDiagnostic(value []byte) string {
	text := strings.TrimSpace(string(value))
	if len(text) > 128 {
		text = text[:128]
	}
	return text
}
