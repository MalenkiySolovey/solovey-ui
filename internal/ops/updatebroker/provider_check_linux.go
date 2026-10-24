//go:build linux

package updatebroker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"slices"
	"syscall"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/targetcheck"
)

const providerCheckTimeout = 20 * time.Second

type preparedProviderCheck struct {
	ExecutableDigest    string
	ConfigurationDigest string
	ReportDigest        string
	TargetCapabilities  string
	CapabilityRevision  string
}

// checkPreparedProvider is an Update-owned fixed operation. The source and
// complete archive identity were already authenticated by Update, and the same
// root-owned executable object is independently inspected, executed and
// revalidated. Success is evidence only; the maintenance owner must durably
// bind it to a current checkpoint before it can admit activation.
func checkPreparedProvider(ctx context.Context, root string, identity ReleaseIdentityV1, raw []byte, peer broker.PeerIdentity) (preparedProviderCheck, error) {
	object, err := openPreparedProvider(root, identity)
	if err != nil {
		return preparedProviderCheck{}, errors.New("prepared provider configuration check failed")
	}
	defer func() { _ = object.Close() }()
	return checkProviderObject(ctx, object, identity, raw, peer)
}

// The previous side is independently observed through the authenticated peer's
// executable object. Its claimed Panel version/capability revision is accepted
// only after the same fixed, unprivileged command reports it on the same input.
func checkPreviousProvider(ctx context.Context, input ProviderMaintenanceV1, profile string, raw []byte, peer broker.PeerIdentity) (preparedProviderCheck, error) {
	object, err := openPreviousProviderPeer(peer, input, profile)
	if err != nil {
		return preparedProviderCheck{}, err
	}
	defer func() { _ = object.Close() }()
	identity := ReleaseIdentityV1{CoreProvider: input.PreviousProvider, Version: input.PreviousPanelVersion, BinaryProfile: profile}
	check, err := checkProviderObject(ctx, object, identity, raw, peer)
	if err != nil || check.ExecutableDigest != input.PreviousExecutableDigest || check.CapabilityRevision != input.PreviousCapabilityRevision ||
		check.ConfigurationDigest != input.ConfigurationDigest {
		return preparedProviderCheck{}, errors.New("previous provider configuration observation changed")
	}
	return check, nil
}

func checkProviderObject(ctx context.Context, object *executableobject.Object, identity ReleaseIdentityV1, raw []byte, peer broker.PeerIdentity) (preparedProviderCheck, error) {
	failed := errors.New("prepared provider configuration check failed")
	if ctx == nil || ctx.Err() != nil || peer.PID <= 0 || len(peer.Groups) > 64 ||
		len(raw) == 0 || len(raw) > targetcheck.MaxConfigBytes || jsonvalue.StrictObject(raw) != nil {
		return preparedProviderCheck{}, failed
	}
	// Freeze the input before running the candidate and computing its proposition.
	configuration := bytes.Clone(raw)
	defer clear(configuration)
	deadline, cancel := context.WithTimeout(ctx, providerCheckTimeout)
	defer cancel()
	command := exec.CommandContext(deadline, object.ExecPath(0), "core-check")
	command.ExtraFiles = []*os.File{object.File()}
	command.Dir = "/"
	command.Env = []string{"LANG=C", "LC_ALL=C"}
	command.Stdin = bytes.NewReader(configuration)
	// Run as the authenticated Panel principal, including its attested groups.
	// No caller-controlled UID, environment, argv or filesystem path crosses RPC.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL,
		Credential: &syscall.Credential{Uid: peer.UID, Gid: peer.GID, Groups: slices.Clone(peer.Groups)}}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 250 * time.Millisecond
	output := boundedProviderCheckOutput{limit: targetcheck.MaxReportBytes}
	diagnostics := boundedProviderCheckOutput{limit: targetcheck.MaxReportBytes}
	command.Stdout, command.Stderr = &output, &diagnostics
	if command.Run() != nil || deadline.Err() != nil || object.Revalidate() != nil {
		return preparedProviderCheck{}, failed
	}
	sum := sha256.Sum256(configuration)
	configurationDigest := hex.EncodeToString(sum[:])
	if !providerCheckReportMatches(output.Bytes(), identity, configurationDigest) {
		return preparedProviderCheck{}, failed
	}
	reportSum := sha256.Sum256(output.Bytes())
	var report targetcheck.Report
	if decodeStrict(output.Bytes(), &report) != nil {
		return preparedProviderCheck{}, failed
	}
	return preparedProviderCheck{ExecutableDigest: object.Identity().Digest, ConfigurationDigest: configurationDigest,
		ReportDigest: hex.EncodeToString(reportSum[:]), TargetCapabilities: string(mustJSON(report.Capabilities)),
		CapabilityRevision: report.CapabilityRevision}, nil
}

// A future signed candidate may carry a new source pin absent from the running
// Panel's catalog. Its report must match the independently inspected signed
// source exactly; the current Panel's default/catalog must not invent it.
func providerCheckReportMatches(raw []byte, identity ReleaseIdentityV1, digest string) bool {
	if len(raw) == 0 || len(raw) > targetcheck.MaxReportBytes || jsonvalue.StrictObject(raw) != nil ||
		release.ValidateCoreProviderSource(identity.CoreProvider) != nil || !versionPattern.MatchString(identity.Version) || !validDigest(digest) {
		return false
	}
	var report targetcheck.Report
	if decodeStrict(raw, &report) != nil || report.Schema != targetcheck.Schema || report.State != "VALID" ||
		report.ConfigurationDigest != digest || len(report.ReasonCodes) != 0 || report.PanelVersion != identity.Version ||
		report.Capabilities.Validate(report.CoreProvider, identity.BinaryProfile) != nil || report.Capabilities.Revision() != report.CapabilityRevision {
		return false
	}
	return providerIdentityMatchesSource(report.CoreProvider, identity.CoreProvider)
}

type boundedProviderCheckOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (w *boundedProviderCheckOutput) Bytes() []byte { return w.buffer.Bytes() }

func (w *boundedProviderCheckOutput) Write(data []byte) (int, error) {
	if len(data) > w.limit-w.buffer.Len() {
		return 0, errors.New("prepared provider check output exceeded its bound")
	}
	return w.buffer.Write(data)
}
