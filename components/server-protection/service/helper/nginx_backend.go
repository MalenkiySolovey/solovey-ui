package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	nginxDirectoryMode = 0o700
	nginxFileMode      = 0o640
	nginxOutputLimit   = 64 << 10
)

var (
	errNginxOutputLimit = errors.New("nginx output limit exceeded")
	nginxVersionPattern = regexp.MustCompile(`(?m)nginx/([0-9][0-9A-Za-z.+~-]*)`)
)

// NginxExecutor is the complete typed nginx vocabulary of the privileged
// helper. Paths, flags, signals and service names never cross this boundary.
type NginxExecutor interface {
	Detect(context.Context) NginxSupport
	DetectVersion(context.Context) (*NginxVersionResult, error)
	Validate(context.Context, Correlation, NginxValidateRequest) (*NginxResult, error)
	Install(context.Context, Correlation, NginxInstallRequest) (*NginxResult, error)
	Switch(context.Context, Correlation, NginxSwitchRequest) (*NginxResult, error)
	Reload(context.Context, Correlation, NginxReloadRequest) (*NginxResult, error)
	Verify(context.Context, Correlation, NginxVerifyRequest) (*NginxResult, error)
	Restore(context.Context, Correlation, NginxRestoreRequest) (*NginxResult, error)
}

type systemNginxExecutor struct{ root ManagedRoot }

func newSystemNginxExecutor(root ManagedRoot) NginxExecutor { return &systemNginxExecutor{root: root} }

func (e *systemNginxExecutor) Detect(ctx context.Context) NginxSupport {
	result := NginxSupport{PlatformKnown: true, Linux: runtime.GOOS == "linux", Reason: "nginx_linux_required"}
	if !result.Linux {
		return result
	}
	managed, err := e.managedDirectory(true)
	if err != nil {
		result.Reason = "managed_nginx_root_unavailable"
		return result
	}
	loader := filepath.Join(managed, "loader.conf")
	if err := requireManagedRegular(loader); err != nil {
		result.Reason = "controlled_nginx_config_unavailable"
		return result
	}
	identity, version, modules, err := detectSystemNginx(ctx)
	if err != nil {
		result.Reason = safeNginxReason(err)
		return result
	}
	if !containsString(modules, "stream") || !containsString(modules, "ssl_preread") {
		result.Reason = "nginx_stream_capability_unavailable"
		return result
	}
	activeRevision, activeSHA, _ := e.active()
	result.Available = true
	result.Reason = ""
	result.Binary = identity
	result.ManagedRoot = filepath.Clean(managed)
	result.ControlledConfig = filepath.Clean(loader)
	result.ActiveRevision, result.ActiveSHA256 = activeRevision, activeSHA
	if activeRevision != "" {
		_, result.Listeners, _ = e.revisionMetadata(activeRevision)
	}
	if pid, _, processErr := nginxProcess(identity, managed); processErr == nil {
		result.MasterPID = pid
	}
	_ = version
	return result
}

func (e *systemNginxExecutor) DetectVersion(ctx context.Context) (*NginxVersionResult, error) {
	support := e.Detect(ctx)
	if !support.Available {
		return nil, errors.New(support.Reason)
	}
	_, version, modules, err := detectSystemNginx(ctx)
	if err != nil {
		return nil, err
	}
	return &NginxVersionResult{Detected: true, Version: version, Modules: modules, Binary: support.Binary}, nil
}

func (e *systemNginxExecutor) Validate(ctx context.Context, correlation Correlation, request NginxValidateRequest) (*NginxResult, error) {
	support := e.Detect(ctx)
	if !support.Available || support.Binary != request.ExpectedBinary {
		return nil, errors.New("nginx binary or ownership capability changed")
	}
	candidate, err := e.readCandidate(request.CandidatePath, request.ExpectedSHA256)
	if err != nil {
		return nil, err
	}
	managed := support.ManagedRoot
	testDir := filepath.Join(managed, "staging", correlation.OperationID)
	if err := ensurePrivateDirectory(testDir); err != nil {
		return nil, err
	}
	testConfig := filepath.Join(testDir, "nginx.conf")
	config := []byte("pid " + filepath.ToSlash(filepath.Join(testDir, "nginx.pid")) + ";\nevents {}\nstream { include " + filepath.ToSlash(candidate) + "; }\n")
	if err := atomicNginxWrite(testConfig, config); err != nil {
		return nil, err
	}
	args := []string{"-t", "-q", "-p", testDir + string(filepath.Separator), "-c", testConfig}
	stdout, stderr, err := runNginxBounded(ctx, support.Binary.TargetPath, args)
	if err != nil {
		return nil, fmt.Errorf("nginx candidate validation failed: %s", boundedDiagnostic(stdout, stderr))
	}
	return &NginxResult{Revision: request.ExpectedRevision, SHA256: request.ExpectedSHA256, Binary: support.Binary, Diagnostics: []string{"candidate_validation_passed"}}, nil
}

func (e *systemNginxExecutor) Install(_ context.Context, _ Correlation, request NginxInstallRequest) (*NginxResult, error) {
	candidate, err := e.readCandidate(request.CandidatePath, request.ExpectedSHA256)
	if err != nil {
		return nil, err
	}
	managed, err := e.managedDirectory(true)
	if err != nil {
		return nil, err
	}
	revisions := filepath.Join(managed, "revisions")
	if err := ensurePrivateDirectory(revisions); err != nil {
		return nil, err
	}
	revisionDir := filepath.Join(revisions, request.ExpectedRevision)
	if info, statErr := os.Lstat(revisionDir); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !platformRootOwned(info) || info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("managed nginx revision is not an immutable directory")
		}
		sha, readErr := readRevisionSHA(revisionDir)
		if readErr != nil || sha != request.ExpectedSHA256 {
			return nil, errors.New("managed nginx revision already exists with different content")
		}
		return &NginxResult{Revision: request.ExpectedRevision, SHA256: sha}, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	staging := revisionDir + ".staging"
	if err := os.RemoveAll(staging); err != nil {
		return nil, err
	}
	if err := ensurePrivateDirectory(staging); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		return nil, err
	}
	if err := atomicNginxWrite(filepath.Join(staging, "stream.conf"), data); err != nil {
		return nil, err
	}
	metadata, _ := json.Marshal(struct {
		Revision  string          `json:"revision"`
		SHA256    string          `json:"sha256"`
		Listeners []NginxListener `json:"listeners"`
	}{request.ExpectedRevision, request.ExpectedSHA256, append([]NginxListener(nil), request.Listeners...)})
	if err := atomicNginxWrite(filepath.Join(staging, "revision.json"), append(metadata, '\n')); err != nil {
		return nil, err
	}
	if err := syncDirectory(staging); err != nil {
		return nil, err
	}
	if err := os.Rename(staging, revisionDir); err != nil {
		return nil, err
	}
	if err := syncDirectory(revisions); err != nil {
		return nil, err
	}
	return &NginxResult{Revision: request.ExpectedRevision, SHA256: request.ExpectedSHA256}, nil
}

func (e *systemNginxExecutor) Switch(_ context.Context, _ Correlation, request NginxSwitchRequest) (*NginxResult, error) {
	previous, previousSHA, err := e.active()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if previous != request.ExpectedPreviousRevision {
		return nil, errors.New("active nginx revision changed")
	}
	if err := e.switchActive(request.TargetRevision, request.ExpectedSHA256); err != nil {
		return nil, err
	}
	return &NginxResult{Revision: request.TargetRevision, SHA256: request.ExpectedSHA256, PreviousRevision: previous, PreviousSHA256: previousSHA}, nil
}

func (e *systemNginxExecutor) Reload(ctx context.Context, correlation Correlation, request NginxReloadRequest) (*NginxResult, error) {
	support := e.Detect(ctx)
	if !support.Available || support.Binary != request.ExpectedBinary {
		return nil, errors.New("nginx binary or ownership capability changed")
	}
	if err := e.verifyActive(request.ExpectedRevision, request.ExpectedSHA256); err != nil {
		return nil, err
	}
	managed := support.ManagedRoot
	beforePID, beforeWorkers, err := nginxProcess(support.Binary, managed)
	if err != nil {
		return nil, err
	}
	operationDir := filepath.Join(managed, "operations", correlation.OperationID)
	if err := ensurePrivateDirectory(operationDir); err != nil {
		return nil, err
	}
	reloadPath := filepath.Join(operationDir, "reload-"+request.ExpectedRevision+".json")
	var recorded struct {
		State         string `json:"state"`
		MasterPID     int    `json:"masterPid"`
		BeforeWorkers []int  `json:"beforeWorkers"`
		WorkerPIDs    []int  `json:"workerPids"`
	}
	if data, readErr := os.ReadFile(reloadPath); readErr == nil {
		if json.Unmarshal(data, &recorded) != nil || recorded.MasterPID != beforePID {
			return nil, errors.New("nginx reload journal identity mismatch")
		}
		if recorded.State == "completed" {
			return &NginxResult{Revision: request.ExpectedRevision, SHA256: request.ExpectedSHA256, Binary: support.Binary, MasterPID: recorded.MasterPID, WorkerPIDs: append([]int(nil), recorded.WorkerPIDs...), Diagnostics: []string{"reload_idempotent_replay"}}, nil
		}
		if recorded.State == "intent" {
			recoveredPID, recoveredWorkers := beforePID, beforeWorkers
			if equalInts(recorded.BeforeWorkers, beforeWorkers) {
				var waitErr error
				recoveredPID, recoveredWorkers, waitErr = waitForReload(ctx, support.Binary, managed, beforePID, recorded.BeforeWorkers)
				if waitErr != nil {
					return nil, errors.New("nginx reload intent is unresolved; duplicate signal refused")
				}
			}
			recorded.State, recorded.WorkerPIDs = "completed", append([]int(nil), recoveredWorkers...)
			data, _ := json.Marshal(recorded)
			if err := atomicNginxWrite(reloadPath, append(data, '\n')); err != nil {
				return nil, err
			}
			return &NginxResult{Revision: request.ExpectedRevision, SHA256: request.ExpectedSHA256, Binary: support.Binary, MasterPID: recoveredPID, WorkerPIDs: recoveredWorkers, Diagnostics: []string{"reload_recovered_after_restart"}}, nil
		}
		return nil, errors.New("nginx reload journal state is invalid")
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	recorded.State, recorded.MasterPID, recorded.BeforeWorkers = "intent", beforePID, append([]int(nil), beforeWorkers...)
	intent, _ := json.Marshal(recorded)
	if err := atomicNginxWrite(reloadPath, append(intent, '\n')); err != nil {
		return nil, err
	}
	args := []string{"-s", "reload", "-p", managed + string(filepath.Separator), "-c", support.ControlledConfig}
	stdout, stderr, err := runNginxBounded(ctx, support.Binary.TargetPath, args)
	if err != nil {
		return nil, fmt.Errorf("typed nginx reload failed: %s", boundedDiagnostic(stdout, stderr))
	}
	afterPID, afterWorkers, err := waitForReload(ctx, support.Binary, managed, beforePID, beforeWorkers)
	if err != nil {
		return nil, err
	}
	recorded.State, recorded.WorkerPIDs = "completed", append([]int(nil), afterWorkers...)
	completed, _ := json.Marshal(recorded)
	if err := atomicNginxWrite(reloadPath, append(completed, '\n')); err != nil {
		return nil, err
	}
	return &NginxResult{Revision: request.ExpectedRevision, SHA256: request.ExpectedSHA256, Binary: support.Binary, MasterPID: afterPID, WorkerPIDs: afterWorkers, Diagnostics: []string{"reload_verified"}}, nil
}

func waitForReload(ctx context.Context, identity BinaryIdentity, managed string, master int, workers []int) (int, []int, error) {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		case <-timer.C:
			return 0, nil, errors.New("nginx master/worker reload verification failed")
		case <-ticker.C:
			pid, current, err := nginxProcess(identity, managed)
			if err == nil && pid == master && !equalInts(workers, current) {
				return pid, current, nil
			}
		}
	}
}

func (e *systemNginxExecutor) Verify(ctx context.Context, _ Correlation, request NginxVerifyRequest) (*NginxResult, error) {
	support := e.Detect(ctx)
	if !support.Available || support.Binary != request.ExpectedBinary {
		return nil, errors.New("nginx binary or ownership capability changed")
	}
	if err := e.verifyActive(request.ExpectedRevision, request.ExpectedSHA256); err != nil {
		return nil, err
	}
	pid, workers, err := nginxProcess(support.Binary, support.ManagedRoot)
	if err != nil {
		return nil, err
	}
	owners := append([]int{pid}, workers...)
	if err := platformNginxOwnsListeners(owners, request.Listeners); err != nil {
		return nil, err
	}
	return &NginxResult{Revision: request.ExpectedRevision, SHA256: request.ExpectedSHA256, Binary: support.Binary, MasterPID: pid, WorkerPIDs: workers, ListenersMatched: true, Diagnostics: []string{"active_revision_verified", "process_identity_verified", "listeners_verified"}}, nil
}

func (e *systemNginxExecutor) Restore(_ context.Context, _ Correlation, request NginxRestoreRequest) (*NginxResult, error) {
	current, currentSHA, err := e.active()
	if err != nil || current != request.ExpectedCurrentRevision {
		return nil, errors.New("wrong current nginx revision for rollback")
	}
	previousSHA, err := e.revisionSHA(request.PreviousRevision)
	if err != nil || previousSHA != request.ExpectedSHA256 {
		return nil, errors.New("wrong previous nginx revision for rollback")
	}
	if err := e.switchActive(request.PreviousRevision, request.ExpectedSHA256); err != nil {
		return nil, err
	}
	return &NginxResult{Revision: request.PreviousRevision, SHA256: request.ExpectedSHA256, PreviousRevision: current, PreviousSHA256: currentSHA}, nil
}

func (e *systemNginxExecutor) managedDirectory(mustExist bool) (string, error) {
	path, err := e.root.Resolve("nginx", mustExist)
	if err != nil {
		return "", err
	}
	if mustExist {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !platformRootOwned(info) || info.Mode().Perm()&0o077 != 0 {
			return "", errors.New("managed nginx root is missing or symlinked")
		}
	}
	return path, nil
}

func (e *systemNginxExecutor) readCandidate(relative, expectedSHA string) (string, error) {
	path, err := e.root.Resolve(relative, true)
	if err != nil {
		return "", err
	}
	if err := requireRegularNoSymlink(path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if sha256Hex(data) != expectedSHA {
		return "", errors.New("nginx candidate SHA-256 mismatch")
	}
	return path, nil
}

func (e *systemNginxExecutor) revisionSHA(revision string) (string, error) {
	managed, err := e.managedDirectory(true)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(managed, "revisions", revision)
	info, err := os.Lstat(dir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !platformRootOwned(info) || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("managed nginx revision is missing or symlinked")
	}
	return readRevisionSHA(dir)
}

func (e *systemNginxExecutor) revisionMetadata(revision string) (string, []NginxListener, error) {
	managed, err := e.managedDirectory(true)
	if err != nil {
		return "", nil, err
	}
	dir := filepath.Join(managed, "revisions", revision)
	sha, err := readRevisionSHA(dir)
	if err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "revision.json"))
	if err != nil {
		return "", nil, err
	}
	var value struct {
		Listeners []NginxListener `json:"listeners"`
	}
	if err := json.Unmarshal(data, &value); err != nil || validateNginxListeners(value.Listeners) != nil {
		return "", nil, errors.New("managed nginx listener metadata is invalid")
	}
	return sha, value.Listeners, nil
}

func (e *systemNginxExecutor) active() (string, string, error) {
	managed, err := e.managedDirectory(true)
	if err != nil {
		return "", "", err
	}
	active := filepath.Join(managed, "active")
	info, err := os.Lstat(active)
	if err != nil {
		return "", "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", "", errors.New("controlled active reference is not a symlink")
	}
	target, err := os.Readlink(active)
	if err != nil || filepath.IsAbs(target) {
		return "", "", errors.New("controlled active reference is invalid")
	}
	clean := filepath.Clean(target)
	parts := strings.Split(filepath.ToSlash(clean), "/")
	if len(parts) != 2 || parts[0] != "revisions" || !validRevision(parts[1]) {
		return "", "", errors.New("controlled active reference escapes revisions")
	}
	sha, err := e.revisionSHA(parts[1])
	return parts[1], sha, err
}

func (e *systemNginxExecutor) verifyActive(revision, sha string) error {
	activeRevision, activeSHA, err := e.active()
	if err != nil || activeRevision != revision || activeSHA != sha {
		return errors.New("exact active nginx revision verification failed")
	}
	return nil
}

func (e *systemNginxExecutor) switchActive(revision, sha string) error {
	if currentSHA, err := e.revisionSHA(revision); err != nil || currentSHA != sha {
		return errors.New("target nginx revision identity mismatch")
	}
	managed, err := e.managedDirectory(true)
	if err != nil {
		return err
	}
	temporary := filepath.Join(managed, ".active-"+revision)
	_ = os.Remove(temporary)
	if err := os.Symlink(filepath.Join("revisions", revision), temporary); err != nil {
		return err
	}
	active := filepath.Join(managed, "active")
	if err := replaceSymlink(temporary, active); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncDirectory(managed)
}

func readRevisionSHA(dir string) (string, error) {
	info, err := os.Lstat(dir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !platformRootOwned(info) || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("managed nginx revision directory is unsafe")
	}
	if err := requireManagedRegular(filepath.Join(dir, "stream.conf")); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, "stream.conf"))
	if err != nil {
		return "", err
	}
	sha := sha256Hex(data)
	metadata, err := os.ReadFile(filepath.Join(dir, "revision.json"))
	if err != nil {
		return "", err
	}
	var value struct {
		SHA256 string `json:"sha256"`
	}
	if json.Unmarshal(metadata, &value) != nil || value.SHA256 != sha {
		return "", errors.New("managed nginx revision metadata mismatch")
	}
	return sha, nil
}

func detectSystemNginx(ctx context.Context) (BinaryIdentity, string, []string, error) {
	identities := map[string]BinaryIdentity{}
	for _, path := range []string{"/usr/sbin/nginx", "/usr/local/sbin/nginx", "/usr/bin/nginx", "/usr/local/bin/nginx"} {
		identity, err := nginxBinaryIdentity(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return BinaryIdentity{}, "", nil, err
		}
		identities[identity.TargetPath] = identity
	}
	if len(identities) != 1 {
		return BinaryIdentity{}, "", nil, errors.New("nginx binary identity is missing or ambiguous")
	}
	var identity BinaryIdentity
	for _, value := range identities {
		identity = value
	}
	stdout, stderr, err := runNginxBounded(ctx, identity.TargetPath, []string{"-V"})
	if err != nil {
		return BinaryIdentity{}, "", nil, err
	}
	text := string(append(stdout, stderr...))
	versionMatch := nginxVersionPattern.FindStringSubmatch(text)
	if len(versionMatch) != 2 {
		return BinaryIdentity{}, "", nil, errors.New("nginx version output is malformed")
	}
	modules := []string{}
	if strings.Contains(text, "--with-stream") {
		modules = append(modules, "stream")
	}
	if strings.Contains(text, "--with-stream_ssl_preread_module") {
		modules = append(modules, "ssl_preread")
	}
	sort.Strings(modules)
	return identity, versionMatch[1], modules, nil
}

func nginxBinaryIdentity(path string) (BinaryIdentity, error) {
	info, err := os.Stat(path)
	if err != nil {
		return BinaryIdentity{}, err
	}
	if !info.Mode().IsRegular() {
		return BinaryIdentity{}, errors.New("nginx binary is not regular")
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return BinaryIdentity{}, err
	}
	device, inode := platformFileIdentity(info)
	return BinaryIdentity{Path: filepath.Clean(path), TargetPath: filepath.Clean(target), Device: device, Inode: inode}, nil
}

func nginxProcess(identity BinaryIdentity, managed string) (int, []int, error) {
	data, err := os.ReadFile(filepath.Join(managed, "nginx.pid"))
	if err != nil {
		return 0, nil, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, nil, errors.New("managed nginx pid is invalid")
	}
	exe, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil || filepath.Clean(exe) != identity.TargetPath {
		return 0, nil, errors.New("nginx master binary identity mismatch")
	}
	children, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "task", strconv.Itoa(pid), "children"))
	if err != nil {
		return 0, nil, err
	}
	workers := []int{}
	for _, field := range strings.Fields(string(children)) {
		child, parseErr := strconv.Atoi(field)
		if parseErr == nil && child > 0 {
			childExe, identityErr := os.Readlink(filepath.Join("/proc", strconv.Itoa(child), "exe"))
			if identityErr != nil || filepath.Clean(childExe) != identity.TargetPath {
				return 0, nil, errors.New("nginx worker binary identity mismatch")
			}
			workers = append(workers, child)
		}
	}
	sort.Ints(workers)
	if len(workers) == 0 {
		return 0, nil, errors.New("nginx worker identity is unavailable")
	}
	return pid, workers, nil
}

func runNginxBounded(ctx context.Context, binary string, args []string) ([]byte, []byte, error) {
	stdout, stderr := &boundedWriter{limit: nginxOutputLimit}, &boundedWriter{limit: nginxOutputLimit}
	command := exec.CommandContext(ctx, binary, args...)
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()
	if stdout.exceeded || stderr.exceeded {
		return stdout.Bytes(), stderr.Bytes(), errNginxOutputLimit
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

type boundedWriter struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (w *boundedWriter) Write(value []byte) (int, error) {
	remaining := w.limit - w.Len()
	if remaining <= 0 {
		w.exceeded = true
		return len(value), nil
	}
	if len(value) > remaining {
		_, _ = w.Buffer.Write(value[:remaining])
		w.exceeded = true
		return len(value), nil
	}
	return w.Buffer.Write(value)
}

func boundedDiagnostic(stdout, stderr []byte) string {
	text := strings.TrimSpace(string(append(append([]byte(nil), stdout...), stderr...)))
	if text == "" {
		return "no_safe_diagnostic"
	}
	if len(text) > 512 {
		text = text[:512]
	}
	for _, forbidden := range []string{"/etc/", "/home/", "BEGIN PRIVATE", "password", "token", "cookie"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			return "diagnostic_redacted"
		}
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, text)
}

func atomicNginxWrite(path string, data []byte) error {
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".nginx-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(nginxFileMode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func ensurePrivateDirectory(path string) error {
	parent := filepath.Dir(path)
	if parent != path {
		if info, err := os.Lstat(parent); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlinked managed parent is forbidden")
		}
	}
	if err := os.MkdirAll(path, nginxDirectoryMode); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("managed nginx directory is unsafe")
	}
	return os.Chmod(path, nginxDirectoryMode)
}

func requireRegularNoSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("managed nginx file is symlinked or not regular")
	}
	return nil
}

func requireManagedRegular(path string) error {
	if err := requireRegularNoSymlink(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !platformRootOwned(info) || info.Mode().Perm()&0o027 != 0 {
		return errors.New("managed nginx file ownership or permissions are unsafe")
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func replaceSymlink(temporary, active string) error { return os.Rename(temporary, active) }

func safeNginxReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "nginx_probe_timeout"
	}
	if errors.Is(err, errNginxOutputLimit) {
		return "nginx_probe_output_limit"
	}
	return "nginx_capability_unavailable"
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalNginxListeners(left, right []NginxListener) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy, rightCopy := append([]NginxListener(nil), left...), append([]NginxListener(nil), right...)
	sort.Slice(leftCopy, func(i, j int) bool {
		if leftCopy[i].Port != leftCopy[j].Port {
			return leftCopy[i].Port < leftCopy[j].Port
		}
		return leftCopy[i].Address < leftCopy[j].Address
	})
	sort.Slice(rightCopy, func(i, j int) bool {
		if rightCopy[i].Port != rightCopy[j].Port {
			return rightCopy[i].Port < rightCopy[j].Port
		}
		return rightCopy[i].Address < rightCopy[j].Address
	})
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}
func copyBounded(reader io.Reader, limit int64) ([]byte, error) {
	var output bytes.Buffer
	written, err := io.CopyN(&output, reader, limit+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if written > limit {
		return nil, errNginxOutputLimit
	}
	return output.Bytes(), nil
}
