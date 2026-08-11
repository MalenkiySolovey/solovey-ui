//go:build linux

package helper

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	"golang.org/x/sys/unix"
)

const (
	maxOwnerExecutableBytes = int64(256 << 20)
	maxOwnerUnitBytes       = int64(1 << 20)
)

type systemListenerOwnerExecutor struct{}

func newSystemListenerOwnerExecutor() ListenerOwnerExecutor { return systemListenerOwnerExecutor{} }

func (systemListenerOwnerExecutor) Detect(context.Context) ListenerOwnerSupport {
	contract, err := deploymentidentity.LoadInstalled()
	if err != nil {
		return ListenerOwnerSupport{PlatformKnown: true, Linux: true, Reason: "listener_owner_contract_unavailable", ObserverRevision: listenerOwnerObserverDigest()}
	}
	info, err := os.Stat("/usr/bin/systemctl")
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return ListenerOwnerSupport{PlatformKnown: true, Linux: true, Reason: "listener_owner_systemd_unavailable", ContractRevision: contract.Revision, ObserverRevision: listenerOwnerObserverDigest()}
	}
	return ListenerOwnerSupport{PlatformKnown: true, Linux: true, Available: true, ContractRevision: contract.Revision, ObserverRevision: listenerOwnerObserverDigest()}
}

func (systemListenerOwnerExecutor) Observe(ctx context.Context, request ListenerOwnerObserveRequest) (*ListenerOwnerObserveResult, error) {
	result := &ListenerOwnerObserveResult{Facts: []hostfacts.ListenerOwnerFactV1{}}
	contract, err := deploymentidentity.LoadInstalled()
	if err != nil {
		result.ReasonCodes = []string{"listener_owner_contract_unavailable"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	if !ownerRequestMatchesContract(request, contract) {
		result.ReasonCodes = []string{"listener_deployment_mismatch"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	before, err := observeSystemdService(ctx, contract)
	if err != nil {
		result.ReasonCodes = []string{"listener_service_unavailable"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	pidfd, err := unix.PidfdOpen(before.MainPID, 0)
	if err != nil {
		result.ReasonCodes = []string{"listener_owner_capability_unavailable"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	defer unix.Close(pidfd)
	process, err := observeExactProcess(ctx, before.MainPID, contract)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		result.ReasonCodes = []string{"listener_process_identity_mismatch"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	sockets, reasons, err := observeProcessListeners(ctx, pidfd, before.MainPID, request)
	if err != nil {
		return nil, err
	}
	result.ReasonCodes = append(result.ReasonCodes, reasons...)
	after, err := observeSystemdService(ctx, contract)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		result.ReasonCodes = append(result.ReasonCodes, "listener_owner_stale")
		sealListenerOwnerResult(result)
		return result, nil
	}
	if before != after {
		result.ReasonCodes = append(result.ReasonCodes, "listener_owner_stale")
		sealListenerOwnerResult(result)
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	currentStart, err := readProcessStart(before.MainPID)
	if err != nil || currentStart != process.StartTime {
		result.ReasonCodes = append(result.ReasonCodes, "listener_owner_stale")
		sealListenerOwnerResult(result)
		return result, nil
	}
	if len(sockets) == 0 {
		if len(result.ReasonCodes) == 0 {
			result.ReasonCodes = append(result.ReasonCodes, "listener_unobserved")
		}
		sealListenerOwnerResult(result)
		return result, nil
	}
	if duplicateOwnerCoverage(sockets) {
		result.ReasonCodes = append(result.ReasonCodes, "listener_owner_ambiguous")
		sealListenerOwnerResult(result)
		return result, nil
	}
	now := time.Now().UTC()
	pid := before.MainPID
	service := hostfacts.ServiceFact{
		SystemdUnit: contract.SystemdUnit, MainPID: &pid, FragmentPath: before.FragmentPath,
		FragmentSHA256: before.FragmentSHA256,
		ActiveState:    before.ActiveState, SubState: before.SubState, ControlGroup: before.ControlGroup,
		StartMonotonicUsec: before.StartMonotonicUsec,
	}
	for _, socket := range sockets {
		fact := hostfacts.ListenerOwnerFactV1{
			Schema: hostfacts.ListenerOwnerFactSchemaV1, Socket: socket, Process: process, Service: service,
			Application: hostfacts.ListenerApplicationIdentityV1{
				InstanceID: contract.InstanceID, SourceRevision: contract.SourceRevision,
				ArtifactRevision: contract.ArtifactRevision, DeploymentID: contract.DeploymentID,
				OwnerContractRevision: contract.Revision, RuntimeRootBindingRevision: contract.RuntimeRootBindingRevision,
				ExpectedExecutableSHA256: contract.ExecutableSHA256, ServiceIdentity: contract.ServiceIdentity,
				ResourceID: request.ResourceID, ResourceOwnerRevision: request.ExpectedResourceOwnerRevision,
				ConfigurationRevision: request.ExpectedConfigurationRevision,
			},
			ObservedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
		}
		fact.Seal()
		result.Facts = append(result.Facts, fact)
	}
	sealListenerOwnerResult(result)
	return result, nil
}

func ownerRequestMatchesContract(request ListenerOwnerObserveRequest, contract deploymentidentity.ApplicationOwnerContractV1) bool {
	return request.ExpectedInstanceID == contract.InstanceID && request.ExpectedSourceRevision == contract.SourceRevision &&
		request.ExpectedArtifactRevision == contract.ArtifactRevision && request.ExpectedDeploymentID == contract.DeploymentID &&
		request.ExpectedOwnerContractRevision == contract.Revision && request.ExpectedRuntimeRootBindingRevision == contract.RuntimeRootBindingRevision
}

type systemdOwnerSnapshot struct {
	MainPID            int
	ActiveState        string
	SubState           string
	ControlGroup       string
	FragmentPath       string
	FragmentSHA256     string
	StartMonotonicUsec uint64
}

func observeSystemdService(ctx context.Context, contract deploymentidentity.ApplicationOwnerContractV1) (systemdOwnerSnapshot, error) {
	properties := []string{"MainPID", "ActiveState", "SubState", "ControlGroup", "FragmentPath", "ExecMainStartTimestampMonotonic"}
	arguments := []string{"show", contract.SystemdUnit, "--no-page"}
	for _, property := range properties {
		arguments = append(arguments, "--property="+property)
	}
	command := exec.CommandContext(ctx, "/usr/bin/systemctl", arguments...)
	stdout, stderr := &boundedBuffer{limit: MaxOutputBytes}, &boundedBuffer{limit: MaxOutputBytes}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil || stdout.truncated || stderr.truncated {
		return systemdOwnerSnapshot{}, errors.New("bounded systemd owner observation failed")
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(stdout.buffer.Bytes()), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || values[key] != "" {
			continue
		}
		values[key] = value
	}
	mainPID, err := strconv.Atoi(values["MainPID"])
	start, startErr := strconv.ParseUint(values["ExecMainStartTimestampMonotonic"], 10, 64)
	if err != nil || startErr != nil || mainPID <= 1 || values["ActiveState"] != "active" || values["SubState"] != "running" ||
		values["ControlGroup"] != contract.ServiceControlGroup || values["FragmentPath"] != contract.ServiceFragmentPath {
		return systemdOwnerSnapshot{}, errors.New("systemd service identity differs")
	}
	fragmentSHA, err := observeServiceFragment(contract)
	if err != nil {
		return systemdOwnerSnapshot{}, err
	}
	return systemdOwnerSnapshot{mainPID, values["ActiveState"], values["SubState"], values["ControlGroup"], values["FragmentPath"], fragmentSHA, start}, nil
}

func observeServiceFragment(contract deploymentidentity.ApplicationOwnerContractV1) (string, error) {
	before := unix.Stat_t{}
	if err := unix.Lstat(contract.ServiceFragmentPath, &before); err != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Gid != 0 || before.Mode&0o022 != 0 || before.Size <= 0 || before.Size > maxOwnerUnitBytes {
		return "", errors.New("service unit fragment is unsafe")
	}
	file, err := os.Open(contract.ServiceFragmentPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	after := unix.Stat_t{}
	if err := unix.Fstat(int(file.Fd()), &after); err != nil || before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || before.Mtim != after.Mtim {
		return "", errors.New("service unit fragment changed while opening")
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maxOwnerUnitBytes+1))
	if err != nil || written != before.Size || written > maxOwnerUnitBytes {
		return "", errors.New("service unit fragment read failed")
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if digest != contract.ServiceUnitSHA256 {
		return "", errors.New("service unit fragment identity differs")
	}
	return digest, nil
}

func observeExactProcess(ctx context.Context, pid int, contract deploymentidentity.ApplicationOwnerContractV1) (hostfacts.ProcessFact, error) {
	stat, err := readProcessStat(pid)
	if err != nil {
		return hostfacts.ProcessFact{}, err
	}
	uid, gid, err := readProcessCredentials(pid)
	if err != nil || uint32(uid) != contract.ProcessUID || uint32(gid) != contract.ProcessGID {
		return hostfacts.ProcessFact{}, errors.New("process credentials differ")
	}
	cgroup, err := readUnifiedCgroup(pid)
	if err != nil || cgroup != contract.ServiceControlGroup {
		return hostfacts.ProcessFact{}, errors.New("process cgroup differs")
	}
	link := fmt.Sprintf("/proc/%d/exe", pid)
	executableBefore, err := os.Readlink(link)
	if err != nil || executableBefore != contract.ExecutablePath {
		return hostfacts.ProcessFact{}, errors.New("process executable path differs")
	}
	file, err := os.Open(link)
	if err != nil {
		return hostfacts.ProcessFact{}, err
	}
	defer file.Close()
	opened := unix.Stat_t{}
	if err := unix.Fstat(int(file.Fd()), &opened); err != nil || opened.Mode&unix.S_IFMT != unix.S_IFREG {
		return hostfacts.ProcessFact{}, errors.New("process executable is not a regular file")
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(contextReader{ctx: ctx, reader: file}, maxOwnerExecutableBytes+1))
	if err != nil || written > maxOwnerExecutableBytes {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return hostfacts.ProcessFact{}, err
		}
		return hostfacts.ProcessFact{}, errors.New("process executable exceeds the bounded hash limit")
	}
	if err := ctx.Err(); err != nil {
		return hostfacts.ProcessFact{}, err
	}
	executableSHA := hex.EncodeToString(hash.Sum(nil))
	pathStat := unix.Stat_t{}
	if err := unix.Stat(contract.ExecutablePath, &pathStat); err != nil || pathStat.Dev != opened.Dev || pathStat.Ino != opened.Ino || executableSHA != contract.ExecutableSHA256 {
		return hostfacts.ProcessFact{}, errors.New("process executable identity differs")
	}
	executableAfter, err := os.Readlink(link)
	if err != nil || executableAfter != executableBefore {
		return hostfacts.ProcessFact{}, errors.New("process executable changed while observing")
	}
	return hostfacts.ProcessFact{
		PID: &pid, ParentPID: &stat.ParentPID, SessionID: &stat.SessionID, StartTime: stat.StartTime,
		ExeDigest: executableSHA, Executable: executableBefore, ExeDevice: uint64(opened.Dev), ExeInode: opened.Ino,
		UID: &uid, GID: &gid, ControlGroup: cgroup,
	}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

type procOwnerStat struct {
	ParentPID int
	SessionID int
	StartTime string
}

func readProcessStat(pid int) (procOwnerStat, error) {
	data, err := readOwnerFile(fmt.Sprintf("/proc/%d/stat", pid), 8192)
	if err != nil {
		return procOwnerStat{}, err
	}
	closing := strings.LastIndex(string(data), ")")
	if closing < 0 {
		return procOwnerStat{}, errors.New("process stat is malformed")
	}
	fields := strings.Fields(string(data)[closing+1:])
	if len(fields) <= 19 {
		return procOwnerStat{}, errors.New("process stat is incomplete")
	}
	parent, errParent := strconv.Atoi(fields[1])
	session, errSession := strconv.Atoi(fields[3])
	if errParent != nil || errSession != nil || !numericString(fields[19]) {
		return procOwnerStat{}, errors.New("process stat identity is malformed")
	}
	return procOwnerStat{parent, session, fields[19]}, nil
}

func readProcessStart(pid int) (string, error) {
	value, err := readProcessStat(pid)
	return value.StartTime, err
}

func readProcessCredentials(pid int) (int, int, error) {
	data, err := readOwnerFile(fmt.Sprintf("/proc/%d/status", pid), 64<<10)
	if err != nil {
		return 0, 0, err
	}
	uid, gid := -1, -1
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		switch fields[0] {
		case "Uid:":
			uid, _ = strconv.Atoi(fields[2])
		case "Gid:":
			gid, _ = strconv.Atoi(fields[2])
		}
	}
	if uid < 0 || gid < 0 {
		return 0, 0, errors.New("process credentials are absent")
	}
	return uid, gid, nil
}

func readUnifiedCgroup(pid int) (string, error) {
	data, err := readOwnerFile(fmt.Sprintf("/proc/%d/cgroup", pid), 64<<10)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 && parts[0] == "0" && parts[1] == "" && strings.HasPrefix(parts[2], "/") && filepath.Clean(parts[2]) == parts[2] {
			return parts[2], nil
		}
	}
	return "", errors.New("unified process cgroup is absent")
}

func observeProcessListeners(ctx context.Context, pidfd, pid int, request ListenerOwnerObserveRequest) ([]hostfacts.ListenerSocketIdentityV1, []string, error) {
	directory, err := os.Open(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		return nil, []string{"listener_owner_unavailable"}, nil
	}
	defer directory.Close()
	names, err := directory.Readdirnames(4097)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, []string{"listener_owner_unavailable"}, nil
	}
	if len(names) > 4096 {
		return nil, []string{"listener_owner_scan_bounded"}, nil
	}
	sort.Strings(names)
	result := make([]hostfacts.ListenerSocketIdentityV1, 0)
	seen := map[uint64]bool{}
	duplicated := 0
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		targetFD, err := strconv.Atoi(name)
		if err != nil || targetFD < 0 {
			continue
		}
		fd, err := unix.PidfdGetfd(pidfd, targetFD, 0)
		if err != nil {
			continue
		}
		duplicated++
		socket, ok := inspectDuplicatedSocket(fd, request)
		unix.Close(fd)
		if !ok || seen[socket.Cookie] {
			continue
		}
		seen[socket.Cookie] = true
		result = append(result, socket)
	}
	if len(names) > 0 && duplicated == 0 {
		return nil, []string{"listener_owner_capability_unavailable"}, nil
	}
	return result, nil, nil
}

func inspectDuplicatedSocket(fd int, request ListenerOwnerObserveRequest) (hostfacts.ListenerSocketIdentityV1, bool) {
	stat := unix.Stat_t{}
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFSOCK || stat.Ino == 0 {
		return hostfacts.ListenerSocketIdentityV1{}, false
	}
	typeValue, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || request.Network == "tcp" && typeValue != unix.SOCK_STREAM || request.Network == "udp" && typeValue != unix.SOCK_DGRAM {
		return hostfacts.ListenerSocketIdentityV1{}, false
	}
	if request.Network == "tcp" {
		accepting, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ACCEPTCONN)
		if err != nil || accepting != 1 {
			return hostfacts.ListenerSocketIdentityV1{}, false
		}
	}
	sockaddr, err := unix.Getsockname(fd)
	if err != nil {
		return hostfacts.ListenerSocketIdentityV1{}, false
	}
	var family hostfacts.Family
	var address netip.Addr
	var port int
	var ipv6Only *bool
	switch value := sockaddr.(type) {
	case *unix.SockaddrInet4:
		family, address, port = hostfacts.FamilyIPv4, netip.AddrFrom4(value.Addr), value.Port
	case *unix.SockaddrInet6:
		family, address, port = hostfacts.FamilyIPv6, netip.AddrFrom16(value.Addr), value.Port
		flag, err := unix.GetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY)
		if err != nil || flag != 0 && flag != 1 {
			return hostfacts.ListenerSocketIdentityV1{}, false
		}
		flagValue := flag == 1
		ipv6Only = &flagValue
	default:
		return hostfacts.ListenerSocketIdentityV1{}, false
	}
	if port != request.Port || !listenerAddressMatches(request, address) {
		return hostfacts.ListenerSocketIdentityV1{}, false
	}
	cookie, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_COOKIE)
	if err != nil || cookie == 0 {
		return hostfacts.ListenerSocketIdentityV1{}, false
	}
	coverage := []hostfacts.Family{family}
	if family == hostfacts.FamilyIPv6 && address.IsUnspecified() && ipv6Only != nil && !*ipv6Only {
		coverage = []hostfacts.Family{hostfacts.FamilyIPv4, hostfacts.FamilyIPv6}
	}
	return hostfacts.ListenerSocketIdentityV1{
		Network: hostfacts.Network(request.Network), Family: family, Bind: address.String(), Port: uint16(port),
		Inode: strconv.FormatUint(stat.Ino, 10), Cookie: cookie, Wildcard: address.IsUnspecified(), IPv6Only: ipv6Only,
		CoverageFamilies: coverage,
	}, true
}

func listenerAddressMatches(request ListenerOwnerObserveRequest, observed netip.Addr) bool {
	if request.ConfiguredMode == "exact" {
		expected, err := netip.ParseAddr(request.ConfiguredAddress)
		return err == nil && expected == observed
	}
	if !observed.IsUnspecified() {
		return false
	}
	switch request.ConfiguredAddress {
	case "*":
		return true
	case "0.0.0.0":
		return observed.Is4()
	case "::":
		return observed.Is6()
	default:
		return false
	}
}

func duplicateOwnerCoverage(sockets []hostfacts.ListenerSocketIdentityV1) bool {
	seen := map[string]bool{}
	for _, socket := range sockets {
		for _, family := range socket.CoverageFamilies {
			key := string(socket.Network) + "\x00" + string(family) + "\x00" + strconv.Itoa(int(socket.Port))
			if seen[key] {
				return true
			}
			seen[key] = true
		}
	}
	return false
}

func readOwnerFile(name string, limit int64) ([]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("owner observation file exceeds its bound")
	}
	return data, nil
}

func numericString(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
