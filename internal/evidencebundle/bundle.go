package evidencebundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	SchemaVersion        = "solovey-evidence-bundle/v2"
	MaxFailures          = 32
	MaxEpochs            = 16
	MaxRecordBytes       = 64 << 10
	MaxCgroupLines       = 32
	MaxCgroupBytes       = 4 << 10
	MaxLogLines          = 256
	MaxLogBytes          = 64 << 10
	CaptureTimeout       = 5 * time.Second
	LogWindowBefore      = 30 * time.Second
	LogWindowAfter       = 10 * time.Second
	armFilename          = "arm.json"
	manifestFilename     = "manifest.json"
	recordsDirectory     = "records"
	checkpointsDirectory = "checkpoints"
	epochsDirectory      = "epochs"
	activeFilename       = "active.json"
	mutationDirectory    = ".mutation"
	DefaultCaptureRoot   = "/run/solovey-ui/evidence-bundle"
)

type Group string

const (
	GroupService              Group = "service"
	GroupInstance             Group = "instance"
	GroupPeerPID              Group = "peer_pid"
	GroupPeerCredentials      Group = "peer_credentials"
	GroupExecutableIdentity   Group = "executable_identity"
	GroupProcessCgroup        Group = "process_cgroup"
	GroupSupervisorProcess    Group = "supervisor_process"
	GroupBootIdentity         Group = "boot_identity"
	GroupProcessStartIdentity Group = "process_start_identity"
	GroupBoundedLogs          Group = "bounded_logs"
)

var groups = []Group{
	GroupService, GroupInstance, GroupPeerPID, GroupPeerCredentials,
	GroupExecutableIdentity, GroupProcessCgroup, GroupSupervisorProcess,
	GroupBootIdentity, GroupProcessStartIdentity, GroupBoundedLogs,
}

type State string

const (
	StateCaptured      State = "CAPTURED"
	StateNotApplicable State = "NOT_APPLICABLE"
	StateCaptureError  State = "CAPTURE_ERROR"
	StateUnstable      State = "UNSTABLE"
)

type Scenario string

const (
	ScenarioPackageLifecycle Scenario = "package-lifecycle-generation"
	ScenarioBrokerReadiness  Scenario = "broker-readiness-first-client"
	ScenarioListenerSurface  Scenario = "listener-hostsurface-attribution"
	ScenarioSSHRecovery      Scenario = "ssh-reconnect-recovery"
	ScenarioFirewallRecovery Scenario = "firewall-rollback-reboot"
	ScenarioStorageRestore   Scenario = "storage-preservation-restore"
)

var scenarios = map[Scenario]struct{}{
	ScenarioPackageLifecycle: {}, ScenarioBrokerReadiness: {}, ScenarioListenerSurface: {},
	ScenarioSSHRecovery: {}, ScenarioFirewallRecovery: {}, ScenarioStorageRestore: {},
}

type TargetIdentity struct {
	IdentityHash        string `json:"identityHash"`
	MachineIDHash       string `json:"machineIdHash,omitempty"`
	OSRelease           string `json:"osRelease"`
	Architecture        string `json:"architecture"`
	OpenWrtRelease      string `json:"openWrtRelease"`
	OpenWrtRevision     string `json:"openWrtRevision"`
	OpenWrtTarget       string `json:"openWrtTarget"`
	OpenWrtSubtarget    string `json:"openWrtSubtarget"`
	PackageArchitecture string `json:"packageArchitecture"`
}

type CheckpointSpec struct {
	Scenario Scenario `json:"scenario"`
	ID       string   `json:"id"`
}

type Contract struct {
	SchemaVersion     string           `json:"schemaVersion"`
	RunID             string           `json:"runId"`
	SourceIdentity    string           `json:"sourceIdentity"`
	ArtifactSHA256    string           `json:"artifactSha256"`
	SourceFingerprint string           `json:"sourceFingerprint"`
	ArtifactIdentity  string           `json:"artifactIdentity"`
	APKSHA256         string           `json:"apkSha256"`
	OperatorSHA256    string           `json:"operatorSha256"`
	Target            TargetIdentity   `json:"target"`
	Scenarios         []Scenario       `json:"scenarios"`
	Checkpoints       []CheckpointSpec `json:"checkpoints"`
	ArmedAt           time.Time        `json:"armedAt"`
	MaxFailures       int              `json:"maxFailures"`
	MaxRecordBytes    int              `json:"maxRecordBytes"`
	MaxCgroupLines    int              `json:"maxCgroupLines"`
	MaxCgroupBytes    int              `json:"maxCgroupBytes"`
	CaptureTimeoutMS  int64            `json:"captureTimeoutMs"`
	LogWindowBeforeMS int64            `json:"logWindowBeforeMs"`
	LogWindowAfterMS  int64            `json:"logWindowAfterMs"`
	MaxLogLines       int              `json:"maxLogLines"`
	MaxLogBytes       int              `json:"maxLogBytes"`
}

type Trigger struct {
	Scenario            Scenario  `json:"scenario"`
	Checkpoint          string    `json:"checkpoint"`
	FailureClass        string    `json:"failureClass"`
	Timestamp           time.Time `json:"timestamp"`
	ControllerTimestamp time.Time `json:"controllerTimestamp"`
	TargetTimestamp     time.Time `json:"targetTimestamp"`
}

type Facts struct {
	Service              *ServiceFact              `json:"service,omitempty"`
	Instance             *InstanceFact             `json:"instance,omitempty"`
	PeerPID              *PeerPIDFact              `json:"peerPid,omitempty"`
	PeerCredentials      *PeerCredentialsFact      `json:"peerCredentials,omitempty"`
	ExecutableIdentity   *ExecutableIdentityFact   `json:"executableIdentity,omitempty"`
	ProcessCgroup        *ProcessCgroupFact        `json:"processCgroup,omitempty"`
	SupervisorProcess    *SupervisorProcessFact    `json:"supervisorProcess,omitempty"`
	BootIdentity         *BootIdentityFact         `json:"bootIdentity,omitempty"`
	ProcessStartIdentity *ProcessStartIdentityFact `json:"processStartIdentity,omitempty"`
	BoundedLogs          *BoundedLogsFact          `json:"boundedLogs,omitempty"`
}

type ServiceFact struct {
	Name string `json:"name"`
}

type InstanceFact struct {
	Name string `json:"name"`
}

type PeerPIDFact struct {
	PID int `json:"pid"`
}

type PeerCredentialsFact struct {
	PID int    `json:"pid"`
	UID uint32 `json:"uid"`
	GID uint32 `json:"gid"`
}

type ExecutableIdentityFact struct {
	Label  string `json:"label"`
	SHA256 string `json:"sha256"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Mode   uint32 `json:"mode"`
	UID    uint32 `json:"uid"`
	GID    uint32 `json:"gid"`
	Role   string `json:"role"`
}

type ProcessCgroupFact struct {
	Availability      string   `json:"availability"`
	Policy            string   `json:"policy"`
	Unit              string   `json:"unit,omitempty"`
	SupervisorCgroup  string   `json:"supervisorCgroup,omitempty"`
	BoundedProcLines  []string `json:"boundedProcLines,omitempty"`
	AuthorityRevision string   `json:"authorityRevision,omitempty"`
}

type SupervisorProcessFact struct {
	Supervisor       string `json:"supervisor"`
	Service          string `json:"service,omitempty"`
	Instance         string `json:"instance,omitempty"`
	PID              int    `json:"pid,omitempty"`
	StartIdentity    string `json:"startIdentity,omitempty"`
	ManifestClient   string `json:"manifestClient,omitempty"`
	ManifestRevision string `json:"manifestRevision,omitempty"`
}

type BootIdentityFact struct {
	BootID string `json:"bootId"`
}

type ProcessStartIdentityFact struct {
	StartIdentity string `json:"startIdentity"`
}

type LogLine struct {
	Timestamp time.Time `json:"timestamp"`
	Class     string    `json:"class"`
	Message   string    `json:"message"`
}

type BoundedLogsFact struct {
	Source      string    `json:"source"`
	WindowStart time.Time `json:"windowStart"`
	WindowEnd   time.Time `json:"windowEnd"`
	Lines       []LogLine `json:"lines"`
}

type Capture struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitempty"`
	Facts  Facts  `json:"facts,omitempty"`
}

type Collector func(context.Context, Trigger) Capture

type Record struct {
	SchemaVersion   string    `json:"schemaVersion"`
	RunID           string    `json:"runId"`
	Scenario        Scenario  `json:"scenario"`
	Checkpoint      string    `json:"checkpoint"`
	EpochSequence   int       `json:"epochSequence"`
	EpochID         string    `json:"epochId"`
	BootID          string    `json:"bootId"`
	FailureSequence int       `json:"failureSequence"`
	FailureClass    string    `json:"failureClass"`
	Timestamp       time.Time `json:"timestamp"`
	Group           Group     `json:"group"`
	State           State     `json:"state"`
	Reason          string    `json:"reason,omitempty"`
	Facts           Facts     `json:"facts,omitempty"`
	Redaction       string    `json:"redaction"`
	SecretScan      string    `json:"secretScan"`
	RecordHash      string    `json:"recordHash"`
}

type RecordSeal struct {
	FailureSequence int    `json:"failureSequence"`
	Group           Group  `json:"group"`
	Filename        string `json:"filename"`
	SHA256          string `json:"sha256"`
}

type CampaignStatus string

const (
	CampaignCompleteSuccess CampaignStatus = "CAMPAIGN_COMPLETE_SUCCESS"
	CampaignCompleteFailure CampaignStatus = "CAMPAIGN_COMPLETE_FAILURE"
	CampaignIncomplete      CampaignStatus = "CAMPAIGN_INCOMPLETE"
	CampaignInvalid         CampaignStatus = "TAMPERED_OR_INVALID"
)

type Manifest struct {
	SchemaVersion     string           `json:"schemaVersion"`
	RunID             string           `json:"runId"`
	SourceIdentity    string           `json:"sourceIdentity"`
	ArtifactSHA256    string           `json:"artifactSha256"`
	SourceFingerprint string           `json:"sourceFingerprint"`
	ArtifactIdentity  string           `json:"artifactIdentity"`
	APKSHA256         string           `json:"apkSha256"`
	OperatorSHA256    string           `json:"operatorSha256"`
	Target            TargetIdentity   `json:"target"`
	Scenarios         []Scenario       `json:"scenarios"`
	Checkpoints       []CheckpointSpec `json:"checkpoints"`
	Status            CampaignStatus   `json:"status"`
	CheckpointCount   int              `json:"checkpointCount"`
	CheckpointRecords []CheckpointSeal `json:"checkpointRecords"`
	Epochs            []EpochSeal      `json:"epochs"`
	FailureCount      int              `json:"failureCount"`
	Records           []RecordSeal     `json:"records"`
	Completeness      string           `json:"completeness"`
	Redaction         string           `json:"redaction"`
	SecretScan        string           `json:"secretScan"`
	FinalizedAt       time.Time        `json:"finalizedAt"`
	BundleHash        string           `json:"bundleHash"`
}

type Recorder struct {
	root     string
	contract Contract
	mu       sync.Mutex
	next     int
}

var (
	safeID                = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	safeCoordinate        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	hexDigest             = regexp.MustCompile(`^[a-f0-9]{64}$`)
	secretPattern         = regexp.MustCompile(`(?i)(password|passwd|token|cookie|authorization|private[ _-]?key|secret)[[:space:]]*[:=][[:space:]]*[^[:space:]]+|-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	recordFilenamePattern = regexp.MustCompile(`^([0-9]{2})-([a-z_]+)\.json$`)
)

func Groups() []Group { return append([]Group(nil), groups...) }

func (r *Recorder) Contract() Contract {
	if r == nil {
		return Contract{}
	}
	contract := r.contract
	contract.Scenarios = append([]Scenario(nil), r.contract.Scenarios...)
	contract.Checkpoints = append([]CheckpointSpec(nil), r.contract.Checkpoints...)
	return contract
}

func Arm(root string, contract Contract) (*Recorder, error) {
	if contract.ArmedAt.IsZero() {
		contract.ArmedAt = time.Now().UTC()
	}
	contract.SchemaVersion = SchemaVersion
	contract.MaxFailures = MaxFailures
	contract.MaxRecordBytes = MaxRecordBytes
	contract.MaxCgroupLines = MaxCgroupLines
	contract.MaxCgroupBytes = MaxCgroupBytes
	contract.CaptureTimeoutMS = CaptureTimeout.Milliseconds()
	contract.LogWindowBeforeMS = LogWindowBefore.Milliseconds()
	contract.LogWindowAfterMS = LogWindowAfter.Milliseconds()
	contract.MaxLogLines = MaxLogLines
	contract.MaxLogBytes = MaxLogBytes
	prepareContract(&contract)
	if err := validateContract(contract); err != nil {
		return nil, err
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		return nil, fmt.Errorf("create run-scoped capture root: %w", err)
	}
	if err := validatePrivateDirectory(root); err != nil {
		return nil, err
	}
	if err := os.Mkdir(filepath.Join(root, recordsDirectory), 0o700); err != nil {
		return nil, err
	}
	if err := validatePrivateDirectory(filepath.Join(root, recordsDirectory)); err != nil {
		return nil, err
	}
	for _, directory := range []string{checkpointsDirectory, epochsDirectory} {
		path := filepath.Join(root, directory)
		if err := os.Mkdir(path, 0o700); err != nil {
			return nil, err
		}
		if err := validatePrivateDirectory(path); err != nil {
			return nil, err
		}
	}
	data, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeExclusive(filepath.Join(root, armFilename), append(data, '\n')); err != nil {
		return nil, fmt.Errorf("capture is already armed or unsafe: %w", err)
	}
	return &Recorder{root: root, contract: contract, next: 1}, nil
}

func OpenArmed(root string) (*Recorder, error) {
	data, err := readBounded(filepath.Join(root, armFilename), MaxRecordBytes)
	if err != nil {
		return nil, err
	}
	var contract Contract
	if err := decodeExact(data, &contract); err != nil {
		return nil, err
	}
	if err := validateContract(contract); err != nil {
		return nil, err
	}
	if err := validatePrivateDirectory(root); err != nil {
		return nil, err
	}
	for _, directory := range []string{recordsDirectory, checkpointsDirectory, epochsDirectory} {
		if err := validatePrivateDirectory(filepath.Join(root, directory)); err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, recordsDirectory))
	if err != nil {
		return nil, err
	}
	maxSequence := 0
	for _, entry := range entries {
		if entry.IsDir() {
			return nil, fmt.Errorf("unexpected capture record %q", entry.Name())
		}
		match := recordFilenamePattern.FindStringSubmatch(entry.Name())
		if len(match) != 3 || !containsGroup(Group(match[2])) {
			return nil, fmt.Errorf("unexpected capture record %q", entry.Name())
		}
		sequence, err := strconv.Atoi(match[1])
		if err != nil || sequence < 1 || sequence > MaxFailures {
			return nil, fmt.Errorf("unexpected capture record %q", entry.Name())
		}
		if sequence > maxSequence {
			maxSequence = sequence
		}
	}
	return &Recorder{root: root, contract: contract, next: maxSequence + 1}, nil
}

func (r *Recorder) CaptureFailure(parent context.Context, trigger Trigger, collectors map[Group]Collector) (resultErr error) {
	if r == nil {
		return errors.New("capture recorder is absent")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	release, err := acquireCampaignMutation(r.root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	if err := ensureCampaignOpen(r.root); err != nil {
		return err
	}
	var epoch EpochArm
	trigger, epoch, err = resolveFailureTrigger(r.root, r.contract, trigger)
	if err != nil {
		return err
	}
	if r.next < 1 || r.next > MaxFailures {
		return errors.New("capture failure cardinality exceeded")
	}
	if _, ok := scenarios[trigger.Scenario]; !ok || !containsScenario(r.contract.Scenarios, trigger.Scenario) {
		return errors.New("capture trigger scenario is not armed")
	}
	if checkpointIndex(r.contract, trigger.Scenario, trigger.Checkpoint) < 0 {
		return errors.New("capture trigger checkpoint is not armed")
	}
	if !safeID.MatchString(trigger.FailureClass) {
		return errors.New("capture failure class is not closed")
	}
	if trigger.Timestamp.IsZero() {
		trigger.Timestamp = time.Now().UTC()
	}
	ctx, cancel := context.WithTimeout(parent, CaptureTimeout)
	defer cancel()
	sequence := r.next
	var result error
	for _, group := range groups {
		capture := Capture{State: StateCaptureError, Reason: "collector_unavailable"}
		if collector := collectors[group]; collector != nil {
			capture = collector(ctx, trigger)
		}
		if ctx.Err() != nil {
			capture = Capture{State: StateCaptureError, Reason: "capture_timeout"}
		}
		record := Record{SchemaVersion: SchemaVersion, RunID: r.contract.RunID, Scenario: trigger.Scenario,
			Checkpoint: trigger.Checkpoint, EpochSequence: epoch.Sequence, EpochID: epoch.ID, BootID: epoch.BootID,
			FailureSequence: sequence, FailureClass: trigger.FailureClass, Timestamp: trigger.Timestamp.UTC(),
			Group: group, State: capture.State, Reason: capture.Reason, Facts: capture.Facts,
			Redaction: "PASS", SecretScan: "PASS"}
		if err := normalizeRecord(&record); err != nil {
			record.State, record.Reason, record.Facts = StateCaptureError, "record_validation_failed", Facts{}
			result = errors.Join(result, err)
		}
		if err := writeRecord(r.root, &record); err != nil {
			return err
		}
		if record.State == StateCaptureError || record.State == StateUnstable {
			result = errors.Join(result, fmt.Errorf("%s is %s", group, record.State))
		}
	}
	if err := writeFailureCheckpoint(r.root, r.contract, trigger, epoch, sequence); err != nil {
		return errors.Join(result, err)
	}
	r.next++
	return result
}

func (r *Recorder) Finalize() (_ Manifest, resultErr error) {
	if r == nil {
		return Manifest{}, errors.New("capture recorder is absent")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	release, err := acquireCampaignMutation(r.root)
	if err != nil {
		return Manifest{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	if err := ensureCampaignOpen(r.root); err != nil {
		return Manifest{}, err
	}
	arms, seals, epochErr := loadEpochs(r.root, r.contract)
	if epochErr == nil && len(arms) == len(seals)+1 {
		_, epochErr = closeEpochLocked(r.root, r.contract)
	} else if epochErr == nil && len(arms) != len(seals) {
		epochErr = errors.New("campaign epoch chain is ambiguous")
	}
	manifest, verificationErr := inspectCampaign(r.root, r.contract)
	verificationErr = errors.Join(epochErr, verificationErr)
	manifest.FinalizedAt = time.Now().UTC()
	manifest.BundleHash = ""
	data, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, err
	}
	manifest.BundleHash = digest(data)
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	if err := writeExclusive(filepath.Join(r.root, manifestFilename), append(data, '\n')); err != nil {
		return Manifest{}, err
	}
	return manifest, verificationErr
}

func Verify(root string, expected Contract) (Manifest, error) {
	armData, err := readBounded(filepath.Join(root, armFilename), MaxRecordBytes)
	if err != nil {
		return Manifest{}, err
	}
	var armed Contract
	if err := decodeExact(armData, &armed); err != nil {
		return Manifest{}, err
	}
	if err := validateContract(armed); err != nil {
		return Manifest{}, err
	}
	prepareContract(&expected)
	if expected.RunID != armed.RunID || expected.SourceIdentity != armed.SourceIdentity || expected.SourceFingerprint != armed.SourceFingerprint ||
		expected.ArtifactIdentity != armed.ArtifactIdentity || expected.ArtifactSHA256 != armed.ArtifactSHA256 || expected.APKSHA256 != armed.APKSHA256 ||
		expected.OperatorSHA256 != armed.OperatorSHA256 || expected.Target != armed.Target || !sameScenarios(expected.Scenarios, armed.Scenarios) ||
		!sameCheckpoints(expected.Checkpoints, armed.Checkpoints) {
		return Manifest{}, errors.New("capture authority binding differs from expected run/source/artifact/target/scenarios")
	}
	manifestData, err := readBounded(filepath.Join(root, manifestFilename), MaxRecordBytes)
	if err != nil {
		return Manifest{}, err
	}
	var sealed Manifest
	if err := decodeExact(manifestData, &sealed); err != nil {
		return Manifest{}, err
	}
	wantHash := sealed.BundleHash
	sealed.BundleHash = ""
	canonical, _ := json.Marshal(sealed)
	if wantHash == "" || digest(canonical) != wantHash {
		return Manifest{}, errors.New("bundle manifest integrity mismatch")
	}
	sealed.BundleHash = wantHash
	current, err := inspectCampaign(root, armed)
	if err != nil {
		return Manifest{}, err
	}
	if sealed.RunID != current.RunID || sealed.SourceIdentity != current.SourceIdentity || sealed.SourceFingerprint != current.SourceFingerprint ||
		sealed.ArtifactIdentity != current.ArtifactIdentity || sealed.ArtifactSHA256 != current.ArtifactSHA256 || sealed.APKSHA256 != current.APKSHA256 ||
		sealed.OperatorSHA256 != current.OperatorSHA256 || sealed.Target != current.Target || !sameScenarios(sealed.Scenarios, current.Scenarios) ||
		!sameCheckpoints(sealed.Checkpoints, current.Checkpoints) || sealed.Status != current.Status || sealed.Completeness != "PASS" ||
		sealed.Redaction != "PASS" || sealed.SecretScan != "PASS" || !sameRecordSeals(sealed.Records, current.Records) ||
		!sameCheckpointSeals(sealed.CheckpointRecords, current.CheckpointRecords) || !sameEpochSeals(sealed.Epochs, current.Epochs) {
		return Manifest{}, errors.New("sealed bundle differs from current complete records")
	}
	if err := rejectUnexpectedCampaignFiles(root); err != nil {
		return Manifest{}, err
	}
	return sealed, nil
}

func normalizeRecord(record *Record) error {
	if record.State != StateCaptured && record.State != StateNotApplicable && record.State != StateCaptureError && record.State != StateUnstable {
		return errors.New("capture state is not closed")
	}
	if record.State == StateNotApplicable {
		if !notApplicableReason(record.Group, record.Reason) || !factsEmpty(record.Facts) {
			return errors.New("NOT_APPLICABLE requires a schema reason and no facts")
		}
		return nil
	}
	if record.State != StateCaptured {
		if !safeID.MatchString(record.Reason) || !factsEmpty(record.Facts) {
			return errors.New("failed capture requires a closed reason and no retained facts")
		}
		return nil
	}
	if record.Reason != "" || !factsMatchGroup(record.Group, record.Facts) {
		return errors.New("captured facts do not match their canonical group")
	}
	data, _ := json.Marshal(record.Facts)
	if len(data) > MaxRecordBytes {
		return errors.New("capture facts exceed the bounded size")
	}
	if secretPattern.Match(data) {
		record.Redaction, record.SecretScan = "FAIL", "FAIL"
		return errors.New("secret-positive evidence rejected")
	}
	return validateFactValues(record.Group, record.Facts)
}

func writeRecord(root string, record *Record) error {
	record.RecordHash = ""
	canonical, err := json.Marshal(record)
	if err != nil {
		return err
	}
	record.RecordHash = digest(canonical)
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > MaxRecordBytes {
		return errors.New("capture record exceeds bounded size")
	}
	name := fmt.Sprintf("%02d-%s.json", record.FailureSequence, record.Group)
	return writeExclusive(filepath.Join(root, recordsDirectory, name), append(data, '\n'))
}

func inspectRecords(root string, contract Contract, failures int) (Manifest, error) {
	manifest := Manifest{SchemaVersion: SchemaVersion, RunID: contract.RunID, SourceIdentity: contract.SourceIdentity,
		ArtifactSHA256: contract.ArtifactSHA256, Target: contract.Target, Scenarios: append([]Scenario(nil), contract.Scenarios...),
		FailureCount: failures, Completeness: "PASS", Redaction: "PASS", SecretScan: "PASS"}
	if failures < 1 || failures > MaxFailures {
		manifest.Completeness = "FAIL"
		return manifest, errors.New("capture failure count is outside bounds")
	}
	var result error
	for sequence := 1; sequence <= failures; sequence++ {
		for _, group := range groups {
			name := fmt.Sprintf("%02d-%s.json", sequence, group)
			data, err := readBounded(filepath.Join(root, recordsDirectory, name), MaxRecordBytes)
			if err != nil {
				manifest.Completeness = "FAIL"
				result = errors.Join(result, fmt.Errorf("missing required record %s: %w", name, err))
				continue
			}
			var record Record
			if err := decodeExact(data, &record); err != nil {
				manifest.Completeness = "FAIL"
				result = errors.Join(result, err)
				continue
			}
			want := record.RecordHash
			record.RecordHash = ""
			canonical, _ := json.Marshal(record)
			if want == "" || digest(canonical) != want || record.RunID != contract.RunID || record.SchemaVersion != SchemaVersion ||
				record.FailureSequence != sequence || record.Group != group || !containsScenario(contract.Scenarios, record.Scenario) {
				manifest.Completeness = "FAIL"
				result = errors.Join(result, fmt.Errorf("record authority or integrity mismatch: %s", name))
			}
			record.RecordHash = want
			if err := normalizeRecord(&record); err != nil || record.State == StateCaptureError || record.State == StateUnstable {
				manifest.Completeness = "FAIL"
				result = errors.Join(result, fmt.Errorf("record %s is not successful", name), err)
			}
			if record.Redaction != "PASS" {
				manifest.Redaction = "FAIL"
			}
			if record.SecretScan != "PASS" {
				manifest.SecretScan = "FAIL"
			}
			manifest.Records = append(manifest.Records, RecordSeal{FailureSequence: sequence, Group: group,
				Filename: filepath.ToSlash(filepath.Join(recordsDirectory, name)), SHA256: digest(data)})
		}
	}
	return manifest, result
}

func validateContract(contract Contract) error {
	if contract.SchemaVersion != SchemaVersion || !safeID.MatchString(contract.RunID) || !hexDigest.MatchString(contract.SourceIdentity) ||
		!hexDigest.MatchString(contract.SourceFingerprint) || contract.SourceIdentity != contract.SourceFingerprint ||
		!hexDigest.MatchString(contract.ArtifactSHA256) || !hexDigest.MatchString(contract.APKSHA256) || contract.ArtifactSHA256 != contract.APKSHA256 ||
		!safeID.MatchString(contract.ArtifactIdentity) || !hexDigest.MatchString(contract.OperatorSHA256) ||
		!hexDigest.MatchString(contract.Target.IdentityHash) || contract.Target.MachineIDHash != contract.Target.IdentityHash ||
		!safeID.MatchString(contract.Target.OSRelease) || !safeID.MatchString(contract.Target.Architecture) ||
		!safeID.MatchString(contract.Target.OpenWrtRelease) || !safeID.MatchString(contract.Target.OpenWrtRevision) ||
		!safeCoordinate.MatchString(contract.Target.OpenWrtTarget) || !safeCoordinate.MatchString(contract.Target.OpenWrtSubtarget) ||
		!safeID.MatchString(contract.Target.PackageArchitecture) || contract.ArmedAt.IsZero() ||
		contract.MaxFailures != MaxFailures || contract.MaxRecordBytes != MaxRecordBytes || contract.MaxCgroupLines != MaxCgroupLines ||
		contract.MaxCgroupBytes != MaxCgroupBytes || contract.CaptureTimeoutMS != CaptureTimeout.Milliseconds() ||
		contract.LogWindowBeforeMS != LogWindowBefore.Milliseconds() || contract.LogWindowAfterMS != LogWindowAfter.Milliseconds() ||
		contract.MaxLogLines != MaxLogLines || contract.MaxLogBytes != MaxLogBytes || len(contract.Scenarios) == 0 || len(contract.Scenarios) > len(scenarios) ||
		len(contract.Checkpoints) == 0 {
		return errors.New("capture arm contract is invalid")
	}
	seen := make(map[Scenario]bool, len(contract.Scenarios))
	for _, scenario := range contract.Scenarios {
		if _, ok := scenarios[scenario]; !ok || seen[scenario] {
			return errors.New("capture scenario set is invalid")
		}
		seen[scenario] = true
	}
	want := CanonicalCheckpoints(contract.Scenarios)
	if len(want) != len(contract.Checkpoints) {
		return errors.New("capture checkpoint set is invalid")
	}
	for index := range want {
		if contract.Checkpoints[index] != want[index] {
			return errors.New("capture checkpoint set is not canonical for the armed scenarios")
		}
	}
	return nil
}

func validateFactValues(group Group, facts Facts) error {
	data, _ := json.Marshal(facts)
	if len(data) > MaxRecordBytes {
		return errors.New("capture facts exceed size limit")
	}
	for _, value := range factStrings(facts) {
		if value != "" && (len(value) > 512 || strings.ContainsAny(value, "\x00\r\n")) {
			return errors.New("capture fact is not bounded to one safe line")
		}
	}
	switch group {
	case GroupService:
		if !safeID.MatchString(facts.Service.Name) {
			return errors.New("service identity is invalid")
		}
	case GroupInstance:
		if !safeID.MatchString(facts.Instance.Name) {
			return errors.New("instance identity is invalid")
		}
	case GroupPeerPID:
		if facts.PeerPID.PID <= 1 {
			return errors.New("peer PID is invalid")
		}
	case GroupPeerCredentials:
		if facts.PeerCredentials.PID <= 1 {
			return errors.New("peer credential tuple is invalid")
		}
	case GroupExecutableIdentity:
		value := facts.ExecutableIdentity
		if !strings.HasPrefix(value.Label, "/") || !hexDigest.MatchString(value.SHA256) || value.Device == 0 || value.Inode == 0 ||
			value.Mode == 0 || !safeID.MatchString(value.Role) {
			return errors.New("mapped executable identity is invalid")
		}
	case GroupProcessCgroup:
		value := facts.ProcessCgroup
		if !safeID.MatchString(value.Availability) || !safeID.MatchString(value.Policy) || len(value.BoundedProcLines) == 0 || len(value.BoundedProcLines) > MaxCgroupLines {
			return errors.New("process cgroup identity is invalid")
		}
		bytes := 0
		for _, line := range value.BoundedProcLines {
			bytes += len(line) + 1
		}
		if bytes > MaxCgroupBytes {
			return errors.New("process cgroup evidence exceeds byte limit")
		}
	case GroupSupervisorProcess:
		value := facts.SupervisorProcess
		if !safeID.MatchString(value.Supervisor) || value.PID <= 1 || !safeID.MatchString(value.Service) || !safeID.MatchString(value.Instance) ||
			!safeID.MatchString(value.StartIdentity) || !safeID.MatchString(value.ManifestClient) || !hexDigest.MatchString(value.ManifestRevision) {
			return errors.New("supervisor process identity is invalid")
		}
	case GroupBootIdentity:
		if !safeID.MatchString(facts.BootIdentity.BootID) {
			return errors.New("boot identity is invalid")
		}
	case GroupProcessStartIdentity:
		if !safeID.MatchString(facts.ProcessStartIdentity.StartIdentity) {
			return errors.New("process start identity is invalid")
		}
	case GroupBoundedLogs:
		if !safeID.MatchString(facts.BoundedLogs.Source) {
			return errors.New("log source is invalid")
		}
	}
	if group == GroupBoundedLogs && facts.BoundedLogs != nil {
		logs := facts.BoundedLogs
		if len(logs.Lines) > MaxLogLines || logs.WindowEnd.Before(logs.WindowStart) || logs.WindowEnd.Sub(logs.WindowStart) > LogWindowBefore+LogWindowAfter {
			return errors.New("log capture exceeds its event/time bounds")
		}
		bytes := 0
		for _, line := range logs.Lines {
			bytes += len(line.Class) + len(line.Message)
			if line.Timestamp.Before(logs.WindowStart) || line.Timestamp.After(logs.WindowEnd) || !safeID.MatchString(line.Class) || len(line.Message) > 1024 {
				return errors.New("bounded log line is invalid")
			}
		}
		if bytes > MaxLogBytes {
			return errors.New("log capture exceeds byte limit")
		}
	}
	return nil
}

func factsMatchGroup(group Group, facts Facts) bool {
	nonNil := 0
	if facts.Service != nil {
		nonNil++
	}
	if facts.Instance != nil {
		nonNil++
	}
	if facts.PeerPID != nil {
		nonNil++
	}
	if facts.PeerCredentials != nil {
		nonNil++
	}
	if facts.ExecutableIdentity != nil {
		nonNil++
	}
	if facts.ProcessCgroup != nil {
		nonNil++
	}
	if facts.SupervisorProcess != nil {
		nonNil++
	}
	if facts.BootIdentity != nil {
		nonNil++
	}
	if facts.ProcessStartIdentity != nil {
		nonNil++
	}
	if facts.BoundedLogs != nil {
		nonNil++
	}
	if nonNil != 1 {
		return false
	}
	switch group {
	case GroupService:
		return facts.Service != nil
	case GroupInstance:
		return facts.Instance != nil
	case GroupPeerPID:
		return facts.PeerPID != nil
	case GroupPeerCredentials:
		return facts.PeerCredentials != nil
	case GroupExecutableIdentity:
		return facts.ExecutableIdentity != nil
	case GroupProcessCgroup:
		return facts.ProcessCgroup != nil
	case GroupSupervisorProcess:
		return facts.SupervisorProcess != nil
	case GroupBootIdentity:
		return facts.BootIdentity != nil
	case GroupProcessStartIdentity:
		return facts.ProcessStartIdentity != nil
	case GroupBoundedLogs:
		return facts.BoundedLogs != nil
	default:
		return false
	}
}

func factsEmpty(facts Facts) bool {
	return facts.Service == nil && facts.Instance == nil && facts.PeerPID == nil && facts.PeerCredentials == nil &&
		facts.ExecutableIdentity == nil && facts.ProcessCgroup == nil && facts.SupervisorProcess == nil &&
		facts.BootIdentity == nil && facts.ProcessStartIdentity == nil && facts.BoundedLogs == nil
}

func factStrings(f Facts) []string {
	var result []string
	if f.Service != nil {
		result = append(result, f.Service.Name)
	}
	if f.Instance != nil {
		result = append(result, f.Instance.Name)
	}
	if f.ExecutableIdentity != nil {
		result = append(result, f.ExecutableIdentity.Label, f.ExecutableIdentity.SHA256, f.ExecutableIdentity.Role)
	}
	if f.ProcessCgroup != nil {
		result = append(result, f.ProcessCgroup.Availability, f.ProcessCgroup.Policy, f.ProcessCgroup.Unit, f.ProcessCgroup.SupervisorCgroup, f.ProcessCgroup.AuthorityRevision)
		result = append(result, f.ProcessCgroup.BoundedProcLines...)
	}
	if f.SupervisorProcess != nil {
		result = append(result, f.SupervisorProcess.Supervisor, f.SupervisorProcess.Service, f.SupervisorProcess.Instance, f.SupervisorProcess.StartIdentity, f.SupervisorProcess.ManifestClient, f.SupervisorProcess.ManifestRevision)
	}
	if f.BootIdentity != nil {
		result = append(result, f.BootIdentity.BootID)
	}
	if f.ProcessStartIdentity != nil {
		result = append(result, f.ProcessStartIdentity.StartIdentity)
	}
	if f.BoundedLogs != nil {
		result = append(result, f.BoundedLogs.Source)
		for _, line := range f.BoundedLogs.Lines {
			result = append(result, line.Class, line.Message)
		}
	}
	return result
}

func notApplicableReason(group Group, reason string) bool {
	allowed := map[string]bool{"scenario_has_no_peer": true, "supervisor_is_not_procd": true, "owner_has_no_log_source": true, "group_not_used_by_scenario": true}
	return allowed[reason] && group != ""
}

func writeExclusive(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	return errors.Join(err, closeErr)
}

func readBounded(path string, limit int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > int64(limit) {
		return nil, fmt.Errorf("untrusted or unbounded evidence file %s", filepath.Base(path))
	}
	if err := validateEvidencePermissions(info); err != nil {
		return nil, err
	}
	if err := validateEvidenceOwner(info); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("capture directory is not private")
	}
	if err := validateEvidencePermissions(info); err != nil {
		return err
	}
	return validateEvidenceOwner(info)
}

func decodeExact(data []byte, value any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("evidence file contains trailing JSON")
	}
	return nil
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func containsScenario(values []Scenario, wanted Scenario) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func containsGroup(wanted Group) bool {
	for _, value := range groups {
		if value == wanted {
			return true
		}
	}
	return false
}
func sameScenarios(left, right []Scenario) bool {
	a, b := append([]Scenario(nil), left...), append([]Scenario(nil), right...)
	sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	sort.Slice(b, func(i, j int) bool { return b[i] < b[j] })
	return fmt.Sprint(a) == fmt.Sprint(b)
}
func sameRecordSeals(left, right []RecordSeal) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}

func sameCheckpoints(left, right []CheckpointSpec) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}

func sameCheckpointSeals(left, right []CheckpointSeal) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}

func sameEpochSeals(left, right []EpochSeal) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}

func rejectUnexpectedFiles(root string, seals []RecordSeal) error {
	expected := map[string]bool{armFilename: true, manifestFilename: true, recordsDirectory: true}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !expected[entry.Name()] {
			return fmt.Errorf("unexpected bundle entry %q", entry.Name())
		}
	}
	recordExpected := make(map[string]bool, len(seals))
	for _, seal := range seals {
		recordExpected[filepath.Base(seal.Filename)] = true
	}
	recordEntries, err := os.ReadDir(filepath.Join(root, recordsDirectory))
	if err != nil {
		return err
	}
	for _, entry := range recordEntries {
		if entry.IsDir() || !recordExpected[entry.Name()] {
			return fmt.Errorf("unexpected record %q", entry.Name())
		}
	}
	if len(recordEntries) != len(recordExpected) {
		return errors.New("record cardinality differs from manifest")
	}
	return nil
}
