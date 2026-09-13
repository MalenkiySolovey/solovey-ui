package evidencebundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type EpochTransition string

const (
	EpochInitial    EpochTransition = "INITIAL"
	EpochReboot     EpochTransition = "REBOOT"
	EpochSysupgrade EpochTransition = "SYSUPGRADE"
)

type ClockState string

const (
	ClockSyncProven   ClockState = "SYNC_PROVEN"
	ClockSkewObserved ClockState = "SKEW_OBSERVED"
	ClockUnverified   ClockState = "UNVERIFIED"
)

type EpochInput struct {
	ID                  string          `json:"id"`
	Transition          EpochTransition `json:"transition"`
	BootID              string          `json:"bootId"`
	PreviousEpochHash   string          `json:"previousEpochHash,omitempty"`
	ControllerTimestamp time.Time       `json:"controllerTimestamp"`
	TargetTimestamp     time.Time       `json:"targetTimestamp"`
	ClockState          ClockState      `json:"clockState"`
	ClockSkewMS         int64           `json:"clockSkewMs"`
}

type EpochArm struct {
	SchemaVersion       string          `json:"schemaVersion"`
	RunID               string          `json:"runId"`
	Sequence            int             `json:"sequence"`
	ID                  string          `json:"id"`
	Transition          EpochTransition `json:"transition"`
	BootID              string          `json:"bootId"`
	PreviousEpochHash   string          `json:"previousEpochHash,omitempty"`
	ControllerTimestamp time.Time       `json:"controllerTimestamp"`
	TargetTimestamp     time.Time       `json:"targetTimestamp"`
	ClockState          ClockState      `json:"clockState"`
	ClockSkewMS         int64           `json:"clockSkewMs"`
	ArmHash             string          `json:"armHash"`
}

type EpochSeal struct {
	Sequence          int      `json:"sequence"`
	ID                string   `json:"id"`
	BootID            string   `json:"bootId"`
	PreviousEpochHash string   `json:"previousEpochHash,omitempty"`
	Files             []string `json:"files"`
	FileSHA256        []string `json:"fileSha256"`
	EpochHash         string   `json:"epochHash"`
}

type CheckpointOutcome string

const (
	CheckpointPass CheckpointOutcome = "PASS"
	CheckpointFail CheckpointOutcome = "FAIL"
)

type CheckpointInput struct {
	Scenario            Scenario  `json:"scenario"`
	Checkpoint          string    `json:"checkpoint"`
	ControllerTimestamp time.Time `json:"controllerTimestamp"`
	TargetTimestamp     time.Time `json:"targetTimestamp"`
	EvidenceClass       string    `json:"evidenceClass"`
	EvidenceSHA256      string    `json:"evidenceSha256"`
}

type GroupCapture struct {
	Group   Group   `json:"group"`
	Capture Capture `json:"capture"`
}

type FailureInput struct {
	FailureClass        string         `json:"failureClass"`
	ControllerTimestamp time.Time      `json:"controllerTimestamp"`
	TargetTimestamp     time.Time      `json:"targetTimestamp"`
	Captures            []GroupCapture `json:"captures"`
}

type CheckpointRecord struct {
	SchemaVersion       string            `json:"schemaVersion"`
	RunID               string            `json:"runId"`
	EpochSequence       int               `json:"epochSequence"`
	EpochID             string            `json:"epochId"`
	BootID              string            `json:"bootId"`
	Scenario            Scenario          `json:"scenario"`
	Checkpoint          string            `json:"checkpoint"`
	Outcome             CheckpointOutcome `json:"outcome"`
	FailureSequence     int               `json:"failureSequence,omitempty"`
	FailureClass        string            `json:"failureClass,omitempty"`
	ControllerTimestamp time.Time         `json:"controllerTimestamp"`
	TargetTimestamp     time.Time         `json:"targetTimestamp"`
	EvidenceClass       string            `json:"evidenceClass"`
	EvidenceSHA256      string            `json:"evidenceSha256"`
	Redaction           string            `json:"redaction"`
	SecretScan          string            `json:"secretScan"`
	RecordHash          string            `json:"recordHash"`
}

type CheckpointSeal struct {
	Scenario   Scenario          `json:"scenario"`
	Checkpoint string            `json:"checkpoint"`
	Outcome    CheckpointOutcome `json:"outcome"`
	Filename   string            `json:"filename"`
	SHA256     string            `json:"sha256"`
}

type activeCheckpoint struct {
	SchemaVersion string   `json:"schemaVersion"`
	RunID         string   `json:"runId"`
	EpochSequence int      `json:"epochSequence"`
	EpochID       string   `json:"epochId"`
	BootID        string   `json:"bootId"`
	Scenario      Scenario `json:"scenario"`
	Checkpoint    string   `json:"checkpoint"`
}

var canonicalCheckpoints = map[Scenario][]string{
	ScenarioPackageLifecycle: {
		"package-install", "package-upgrade", "package-reinstall", "package-remove",
		"single-preparation-owner", "generation-handoff", "bounded-stop-remove", "respawn-prerequisites",
	},
	ScenarioBrokerReadiness: {
		"broker-composition", "broker-readiness", "first-client", "peer-credentials",
		"mapped-executable", "cgroup-authority", "manifest-generation", "readiness-exec-continuity",
	},
	ScenarioListenerSurface: {
		"bin-ubus-authority", "legacy-sbin-ubus-absent", "procd-listener-binding",
		"hostsurface-managed-exact", "no-private-listener-taxonomy",
	},
	ScenarioSSHRecovery: {
		"dropbear-lifecycle", "bounded-logread", "independent-reconnect", "rollback",
		"recovery-observation", "management-access-preserved",
	},
	ScenarioFirewallRecovery: {
		"nft-apply-a", "nft-list-a", "nft-apply-b", "nft-rollback", "fw4-coexistence",
		"management-listener-preserved", "reboot-reconciliation", "flush-reconciliation",
	},
	ScenarioStorageRestore: {
		"runtime-root-volatile", "database-root-durable", "sysupgrade-hook-discovered",
		"fresh-preservation-pair", "archive-inventory", "firmware-transition",
		"preservation-restore", "post-reboot-retention",
	},
}

var (
	errCampaignMutationInProgress = errors.New("campaign mutation is already in progress")
	errCampaignFinalized          = errors.New("campaign is already finalized")
)

func CanonicalCheckpoints(selected []Scenario) []CheckpointSpec {
	var result []CheckpointSpec
	for _, scenario := range selected {
		for _, checkpoint := range canonicalCheckpoints[scenario] {
			result = append(result, CheckpointSpec{Scenario: scenario, ID: checkpoint})
		}
	}
	return result
}

func prepareContract(contract *Contract) {
	if contract.SourceFingerprint == "" {
		contract.SourceFingerprint = contract.SourceIdentity
	}
	if contract.SourceIdentity == "" {
		contract.SourceIdentity = contract.SourceFingerprint
	}
	if contract.APKSHA256 == "" {
		contract.APKSHA256 = contract.ArtifactSHA256
	}
	if contract.ArtifactSHA256 == "" {
		contract.ArtifactSHA256 = contract.APKSHA256
	}
	if contract.Target.IdentityHash == "" {
		contract.Target.IdentityHash = contract.Target.MachineIDHash
	}
	if contract.Target.MachineIDHash == "" {
		contract.Target.MachineIDHash = contract.Target.IdentityHash
	}
	if len(contract.Checkpoints) == 0 {
		contract.Checkpoints = CanonicalCheckpoints(contract.Scenarios)
	}
}

func (r *Recorder) BeginEpoch(input EpochInput) (_ EpochArm, resultErr error) {
	if r == nil {
		return EpochArm{}, errors.New("capture recorder is absent")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	release, err := acquireCampaignMutation(r.root)
	if err != nil {
		return EpochArm{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	if err := ensureCampaignOpen(r.root); err != nil {
		return EpochArm{}, err
	}
	if _, err := os.Lstat(filepath.Join(r.root, activeFilename)); err == nil {
		return EpochArm{}, errors.New("an active checkpoint must be completed before an epoch transition")
	} else if !errors.Is(err, os.ErrNotExist) {
		return EpochArm{}, err
	}
	arms, seals, err := loadEpochs(r.root, r.contract)
	if err != nil {
		return EpochArm{}, err
	}
	if len(arms) >= MaxEpochs {
		return EpochArm{}, errors.New("capture epoch cardinality exceeded")
	}
	if len(arms) != len(seals) {
		return EpochArm{}, errors.New("the previous epoch is not integrity-sealed")
	}
	sequence := len(arms) + 1
	previous := ""
	if len(seals) > 0 {
		previous = seals[len(seals)-1].EpochHash
	}
	if input.PreviousEpochHash != previous {
		return EpochArm{}, errors.New("epoch continuation hash differs from the current campaign")
	}
	if sequence == 1 && input.Transition != EpochInitial {
		return EpochArm{}, errors.New("the first campaign epoch must be INITIAL")
	}
	if sequence > 1 && input.Transition != EpochReboot && input.Transition != EpochSysupgrade {
		return EpochArm{}, errors.New("a continued epoch must identify REBOOT or SYSUPGRADE")
	}
	arm := EpochArm{SchemaVersion: SchemaVersion, RunID: r.contract.RunID, Sequence: sequence, ID: input.ID,
		Transition: input.Transition, BootID: input.BootID, PreviousEpochHash: previous,
		ControllerTimestamp: input.ControllerTimestamp.UTC(), TargetTimestamp: input.TargetTimestamp.UTC(),
		ClockState: input.ClockState, ClockSkewMS: input.ClockSkewMS}
	if err := validateEpochArm(arm); err != nil {
		return EpochArm{}, err
	}
	arm.ArmHash = hashJSONWithout(&arm.ArmHash, arm)
	data, _ := json.MarshalIndent(arm, "", "  ")
	if err := writeExclusive(filepath.Join(r.root, epochsDirectory, epochArmFilename(sequence)), append(data, '\n')); err != nil {
		return EpochArm{}, err
	}
	return arm, nil
}

func (r *Recorder) ActivateCheckpoint(scenario Scenario, checkpoint string) (resultErr error) {
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
	if checkpointIndex(r.contract, scenario, checkpoint) < 0 {
		return errors.New("checkpoint is not armed by the campaign contract")
	}
	epoch, err := currentOpenEpoch(r.root, r.contract)
	if err != nil {
		return err
	}
	active := activeCheckpoint{SchemaVersion: SchemaVersion, RunID: r.contract.RunID, EpochSequence: epoch.Sequence,
		EpochID: epoch.ID, BootID: epoch.BootID, Scenario: scenario, Checkpoint: checkpoint}
	data, _ := json.MarshalIndent(active, "", "  ")
	return writeExclusive(filepath.Join(r.root, activeFilename), append(data, '\n'))
}

func (r *Recorder) RecordCheckpoint(input CheckpointInput) (resultErr error) {
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
	active, err := loadActive(r.root, r.contract)
	if err != nil {
		return err
	}
	if active.Scenario != input.Scenario || active.Checkpoint != input.Checkpoint {
		return errors.New("checkpoint record differs from the active transition")
	}
	record := CheckpointRecord{SchemaVersion: SchemaVersion, RunID: r.contract.RunID,
		EpochSequence: active.EpochSequence, EpochID: active.EpochID, BootID: active.BootID,
		Scenario: input.Scenario, Checkpoint: input.Checkpoint, Outcome: CheckpointPass,
		ControllerTimestamp: input.ControllerTimestamp.UTC(), TargetTimestamp: input.TargetTimestamp.UTC(),
		EvidenceClass: input.EvidenceClass, EvidenceSHA256: input.EvidenceSHA256, Redaction: "PASS", SecretScan: "PASS"}
	if err := writeCheckpointRecord(r.root, r.contract, &record); err != nil {
		return err
	}
	return os.Remove(filepath.Join(r.root, activeFilename))
}

func (r *Recorder) RecordFailure(input FailureInput) error {
	if len(input.Captures) != len(groups) || !safeID.MatchString(input.FailureClass) ||
		input.ControllerTimestamp.IsZero() || input.TargetTimestamp.IsZero() {
		return errors.New("bounded failure input is incomplete")
	}
	collectors := make(map[Group]Collector, len(input.Captures))
	for _, item := range input.Captures {
		if !containsGroup(item.Group) || collectors[item.Group] != nil {
			return errors.New("bounded failure input has an invalid group set")
		}
		capture := item.Capture
		collectors[item.Group] = func(_ context.Context, _ Trigger) Capture { return capture }
	}
	return r.CaptureFailure(context.Background(), Trigger{FailureClass: input.FailureClass,
		Timestamp: input.TargetTimestamp, ControllerTimestamp: input.ControllerTimestamp,
		TargetTimestamp: input.TargetTimestamp}, collectors)
}

func (r *Recorder) CloseEpoch() (_ EpochSeal, resultErr error) {
	if r == nil {
		return EpochSeal{}, errors.New("capture recorder is absent")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	release, err := acquireCampaignMutation(r.root)
	if err != nil {
		return EpochSeal{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	if err := ensureCampaignOpen(r.root); err != nil {
		return EpochSeal{}, err
	}
	return closeEpochLocked(r.root, r.contract)
}

func acquireCampaignMutation(root string) (func() error, error) {
	path := filepath.Join(root, mutationDirectory)
	if err := os.Mkdir(path, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, errCampaignMutationInProgress
		}
		return nil, fmt.Errorf("claim campaign mutation: %w", err)
	}
	if err := validatePrivateDirectory(path); err != nil {
		return nil, errors.Join(err, os.Remove(path))
	}
	return func() error {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("release campaign mutation: %w", err)
		}
		return nil
	}, nil
}

func ensureCampaignOpen(root string) error {
	if _, err := os.Lstat(filepath.Join(root, manifestFilename)); err == nil {
		return errCampaignFinalized
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func closeEpochLocked(root string, contract Contract) (EpochSeal, error) {
	if _, err := os.Lstat(filepath.Join(root, activeFilename)); err == nil {
		return EpochSeal{}, errors.New("the epoch has an active incomplete checkpoint")
	} else if !errors.Is(err, os.ErrNotExist) {
		return EpochSeal{}, err
	}
	epoch, err := currentOpenEpoch(root, contract)
	if err != nil {
		return EpochSeal{}, err
	}
	files, hashes, err := epochFiles(root, contract, epoch.Sequence)
	if err != nil {
		return EpochSeal{}, err
	}
	seal := EpochSeal{Sequence: epoch.Sequence, ID: epoch.ID, BootID: epoch.BootID,
		PreviousEpochHash: epoch.PreviousEpochHash, Files: files, FileSHA256: hashes}
	seal.EpochHash = hashJSONWithout(&seal.EpochHash, seal)
	data, _ := json.MarshalIndent(seal, "", "  ")
	if err := writeExclusive(filepath.Join(root, epochsDirectory, epochSealFilename(epoch.Sequence)), append(data, '\n')); err != nil {
		return EpochSeal{}, err
	}
	return seal, nil
}

func resolveFailureTrigger(root string, contract Contract, trigger Trigger) (Trigger, EpochArm, error) {
	active, err := loadActive(root, contract)
	if err != nil {
		return Trigger{}, EpochArm{}, err
	}
	if trigger.Scenario == "" {
		trigger.Scenario = active.Scenario
	}
	if trigger.Checkpoint == "" {
		trigger.Checkpoint = active.Checkpoint
	}
	if trigger.Scenario != active.Scenario || trigger.Checkpoint != active.Checkpoint {
		return Trigger{}, EpochArm{}, errors.New("failure trigger differs from the active transition")
	}
	if trigger.TargetTimestamp.IsZero() {
		trigger.TargetTimestamp = trigger.Timestamp
		if trigger.TargetTimestamp.IsZero() {
			trigger.TargetTimestamp = time.Now().UTC()
		}
	}
	if trigger.ControllerTimestamp.IsZero() {
		trigger.ControllerTimestamp = time.Now().UTC()
	}
	if trigger.Timestamp.IsZero() {
		trigger.Timestamp = trigger.TargetTimestamp
	}
	epoch, err := currentOpenEpoch(root, contract)
	if err != nil {
		return Trigger{}, EpochArm{}, err
	}
	if epoch.Sequence != active.EpochSequence || epoch.ID != active.EpochID || epoch.BootID != active.BootID {
		return Trigger{}, EpochArm{}, errors.New("active checkpoint belongs to another epoch")
	}
	return trigger, epoch, nil
}

func writeFailureCheckpoint(root string, contract Contract, trigger Trigger, epoch EpochArm, sequence int) error {
	evidenceSHA256, err := failureEvidenceDigest(root, sequence)
	if err != nil {
		return err
	}
	record := CheckpointRecord{SchemaVersion: SchemaVersion, RunID: contract.RunID, EpochSequence: epoch.Sequence,
		EpochID: epoch.ID, BootID: epoch.BootID, Scenario: trigger.Scenario, Checkpoint: trigger.Checkpoint,
		Outcome: CheckpointFail, FailureSequence: sequence, FailureClass: trigger.FailureClass,
		ControllerTimestamp: trigger.ControllerTimestamp.UTC(), TargetTimestamp: trigger.TargetTimestamp.UTC(),
		EvidenceClass: "bounded-failure-diagnostics", EvidenceSHA256: evidenceSHA256, Redaction: "PASS", SecretScan: "PASS"}
	if err := writeCheckpointRecord(root, contract, &record); err != nil {
		return err
	}
	return os.Remove(filepath.Join(root, activeFilename))
}

func failureEvidenceDigest(root string, sequence int) (string, error) {
	hash := sha256.New()
	for _, group := range groups {
		name := fmt.Sprintf("%02d-%s.json", sequence, group)
		data, err := readBounded(filepath.Join(root, recordsDirectory, name), MaxRecordBytes)
		if err != nil {
			return "", errors.Join(fmt.Errorf("failure evidence digest is missing %s", name), err)
		}
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeCheckpointRecord(root string, contract Contract, record *CheckpointRecord) error {
	index := checkpointIndex(contract, record.Scenario, record.Checkpoint)
	if index < 0 {
		return errors.New("checkpoint is not part of the armed contract")
	}
	if err := normalizeCheckpointRecord(record); err != nil {
		return err
	}
	record.RecordHash = ""
	canonical, _ := json.Marshal(record)
	record.RecordHash = digest(canonical)
	data, _ := json.MarshalIndent(record, "", "  ")
	if len(data)+1 > MaxRecordBytes {
		return errors.New("checkpoint record exceeds bounded size")
	}
	return writeExclusive(filepath.Join(root, checkpointsDirectory, checkpointFilename(index+1)), append(data, '\n'))
}

func normalizeCheckpointRecord(record *CheckpointRecord) error {
	if record.SchemaVersion != SchemaVersion || !safeID.MatchString(record.RunID) || record.EpochSequence < 1 ||
		!safeID.MatchString(record.EpochID) || !safeID.MatchString(record.BootID) || !safeID.MatchString(string(record.Scenario)) ||
		!safeID.MatchString(record.Checkpoint) || record.ControllerTimestamp.IsZero() || record.TargetTimestamp.IsZero() ||
		!safeID.MatchString(record.EvidenceClass) || !hexDigest.MatchString(record.EvidenceSHA256) {
		return errors.New("checkpoint record is invalid")
	}
	if record.Outcome == CheckpointPass {
		if record.FailureSequence != 0 || record.FailureClass != "" {
			return errors.New("successful checkpoint contains failure identity")
		}
	} else if record.Outcome == CheckpointFail {
		if record.FailureSequence < 1 || record.FailureSequence > MaxFailures || !safeID.MatchString(record.FailureClass) {
			return errors.New("failure checkpoint is incomplete")
		}
	} else {
		return errors.New("checkpoint outcome is not closed")
	}
	data, _ := json.Marshal(record)
	if secretPattern.Match(data) {
		record.Redaction, record.SecretScan = "FAIL", "FAIL"
		return errors.New("secret-positive checkpoint rejected")
	}
	return nil
}

func loadActive(root string, contract Contract) (activeCheckpoint, error) {
	data, err := readBounded(filepath.Join(root, activeFilename), MaxRecordBytes)
	if err != nil {
		return activeCheckpoint{}, errors.Join(errors.New("no checkpoint transition is active"), err)
	}
	var active activeCheckpoint
	if err := decodeExact(data, &active); err != nil {
		return activeCheckpoint{}, err
	}
	if active.SchemaVersion != SchemaVersion || active.RunID != contract.RunID || active.EpochSequence < 1 ||
		!safeID.MatchString(active.EpochID) || !safeID.MatchString(active.BootID) || checkpointIndex(contract, active.Scenario, active.Checkpoint) < 0 {
		return activeCheckpoint{}, errors.New("active checkpoint authority is invalid")
	}
	return active, nil
}

func loadEpochs(root string, contract Contract) ([]EpochArm, []EpochSeal, error) {
	entries, err := os.ReadDir(filepath.Join(root, epochsDirectory))
	if err != nil {
		return nil, nil, err
	}
	var arms []EpochArm
	var seals []EpochSeal
	for _, entry := range entries {
		if entry.IsDir() || strings.Contains(entry.Name(), "..") {
			return nil, nil, fmt.Errorf("unexpected epoch entry %q", entry.Name())
		}
		path := filepath.Join(root, epochsDirectory, entry.Name())
		data, err := readBounded(path, MaxRecordBytes)
		if err != nil {
			return nil, nil, err
		}
		if strings.HasSuffix(entry.Name(), "-arm.json") {
			var arm EpochArm
			if err := decodeExact(data, &arm); err != nil || validateEpochArm(arm) != nil || arm.RunID != contract.RunID {
				return nil, nil, fmt.Errorf("epoch arm is invalid: %s", entry.Name())
			}
			want := arm.ArmHash
			arm.ArmHash = ""
			if want == "" || hashJSONWithout(&arm.ArmHash, arm) != want {
				return nil, nil, errors.New("epoch arm integrity mismatch")
			}
			arm.ArmHash = want
			arms = append(arms, arm)
		} else if strings.HasSuffix(entry.Name(), "-seal.json") {
			var seal EpochSeal
			if err := decodeExact(data, &seal); err != nil {
				return nil, nil, err
			}
			want := seal.EpochHash
			seal.EpochHash = ""
			if want == "" || hashJSONWithout(&seal.EpochHash, seal) != want || len(seal.Files) != len(seal.FileSHA256) {
				return nil, nil, errors.New("epoch seal integrity mismatch")
			}
			seal.EpochHash = want
			seals = append(seals, seal)
		} else {
			return nil, nil, fmt.Errorf("unexpected epoch entry %q", entry.Name())
		}
	}
	sort.Slice(arms, func(i, j int) bool { return arms[i].Sequence < arms[j].Sequence })
	sort.Slice(seals, func(i, j int) bool { return seals[i].Sequence < seals[j].Sequence })
	for i, arm := range arms {
		if arm.Sequence != i+1 || (i == 0 && arm.PreviousEpochHash != "") || (i > 0 && arm.PreviousEpochHash != seals[i-1].EpochHash) {
			return nil, nil, errors.New("epoch arm chain is discontinuous")
		}
		if i < len(seals) {
			seal := seals[i]
			if seal.Sequence != arm.Sequence || seal.ID != arm.ID || seal.BootID != arm.BootID || seal.PreviousEpochHash != arm.PreviousEpochHash {
				return nil, nil, errors.New("epoch seal differs from its arm")
			}
			files, hashes, err := epochFiles(root, contract, arm.Sequence)
			if err != nil || !equalStrings(files, seal.Files) || !equalStrings(hashes, seal.FileSHA256) {
				return nil, nil, errors.New("epoch contents differ from the integrity seal")
			}
		}
	}
	if len(seals) > len(arms) {
		return nil, nil, errors.New("epoch seal has no arm")
	}
	return arms, seals, nil
}

func currentOpenEpoch(root string, contract Contract) (EpochArm, error) {
	arms, seals, err := loadEpochs(root, contract)
	if err != nil {
		return EpochArm{}, err
	}
	if len(arms) == 0 || len(arms) != len(seals)+1 {
		return EpochArm{}, errors.New("campaign has no open epoch")
	}
	return arms[len(arms)-1], nil
}

func validateEpochArm(arm EpochArm) error {
	if arm.SchemaVersion != SchemaVersion || !safeID.MatchString(arm.RunID) || arm.Sequence < 1 || arm.Sequence > MaxEpochs ||
		!safeID.MatchString(arm.ID) || !safeID.MatchString(arm.BootID) || arm.ControllerTimestamp.IsZero() || arm.TargetTimestamp.IsZero() ||
		(arm.Transition != EpochInitial && arm.Transition != EpochReboot && arm.Transition != EpochSysupgrade) ||
		(arm.ClockState != ClockSyncProven && arm.ClockState != ClockSkewObserved && arm.ClockState != ClockUnverified) ||
		(arm.PreviousEpochHash != "" && !hexDigest.MatchString(arm.PreviousEpochHash)) {
		return errors.New("epoch arm is invalid")
	}
	return nil
}

func epochFiles(root string, contract Contract, sequence int) ([]string, []string, error) {
	var files []string
	for _, directory := range []string{checkpointsDirectory, recordsDirectory} {
		entries, err := os.ReadDir(filepath.Join(root, directory))
		if err != nil {
			return nil, nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				return nil, nil, fmt.Errorf("unexpected evidence directory %q", entry.Name())
			}
			path := filepath.Join(root, directory, entry.Name())
			data, err := readBounded(path, MaxRecordBytes)
			if err != nil {
				return nil, nil, err
			}
			belongs := false
			if directory == checkpointsDirectory {
				var record CheckpointRecord
				if err := decodeExact(data, &record); err != nil {
					return nil, nil, err
				}
				belongs = record.EpochSequence == sequence
			} else {
				var record Record
				if err := decodeExact(data, &record); err != nil {
					return nil, nil, err
				}
				belongs = record.EpochSequence == sequence
			}
			if belongs {
				files = append(files, filepath.ToSlash(filepath.Join(directory, entry.Name())))
			}
		}
	}
	sort.Strings(files)
	hashes := make([]string, 0, len(files))
	for _, file := range files {
		data, err := readBounded(filepath.Join(root, filepath.FromSlash(file)), MaxRecordBytes)
		if err != nil {
			return nil, nil, err
		}
		hashes = append(hashes, digest(data))
	}
	return files, hashes, nil
}

func checkpointIndex(contract Contract, scenario Scenario, checkpoint string) int {
	for i, spec := range contract.Checkpoints {
		if spec.Scenario == scenario && spec.ID == checkpoint {
			return i
		}
	}
	return -1
}

func epochArmFilename(sequence int) string  { return fmt.Sprintf("%02d-arm.json", sequence) }
func epochSealFilename(sequence int) string { return fmt.Sprintf("%02d-seal.json", sequence) }
func checkpointFilename(index int) string   { return fmt.Sprintf("%03d.json", index) }

func hashJSONWithout(_ *string, value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func equalStrings(left, right []string) bool {
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

func parseCheckpointOrdinal(name string) (int, error) {
	if len(name) != 8 || !strings.HasSuffix(name, ".json") {
		return 0, errors.New("checkpoint filename is invalid")
	}
	value, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
	if err != nil || value < 1 {
		return 0, errors.New("checkpoint filename is invalid")
	}
	return value, nil
}

func inspectCampaign(root string, contract Contract) (Manifest, error) {
	manifest := Manifest{SchemaVersion: SchemaVersion, RunID: contract.RunID, SourceIdentity: contract.SourceIdentity,
		ArtifactSHA256: contract.ArtifactSHA256, SourceFingerprint: contract.SourceFingerprint,
		ArtifactIdentity: contract.ArtifactIdentity, APKSHA256: contract.APKSHA256, OperatorSHA256: contract.OperatorSHA256,
		Target: contract.Target, Scenarios: append([]Scenario(nil), contract.Scenarios...),
		Checkpoints: append([]CheckpointSpec(nil), contract.Checkpoints...), Status: CampaignIncomplete,
		Completeness: "FAIL", Redaction: "PASS", SecretScan: "PASS"}
	if err := validateContract(contract); err != nil {
		manifest.Status = CampaignInvalid
		return manifest, err
	}
	arms, seals, err := loadEpochs(root, contract)
	if err != nil || len(arms) == 0 || len(arms) != len(seals) {
		manifest.Status = CampaignInvalid
		return manifest, errors.Join(errors.New("campaign epoch chain is not complete"), err)
	}
	manifest.Epochs = append([]EpochSeal(nil), seals...)

	checkpointEntries, err := os.ReadDir(filepath.Join(root, checkpointsDirectory))
	if err != nil {
		manifest.Status = CampaignInvalid
		return manifest, err
	}
	records := make(map[int]CheckpointRecord, len(checkpointEntries))
	for _, entry := range checkpointEntries {
		if entry.IsDir() {
			manifest.Status = CampaignInvalid
			return manifest, fmt.Errorf("unexpected checkpoint entry %q", entry.Name())
		}
		ordinal, err := parseCheckpointOrdinal(entry.Name())
		if err != nil || ordinal > len(contract.Checkpoints) || records[ordinal].RunID != "" {
			manifest.Status = CampaignInvalid
			return manifest, fmt.Errorf("checkpoint cardinality or filename is invalid: %s", entry.Name())
		}
		data, err := readBounded(filepath.Join(root, checkpointsDirectory, entry.Name()), MaxRecordBytes)
		if err != nil {
			manifest.Status = CampaignInvalid
			return manifest, err
		}
		var record CheckpointRecord
		if err := decodeExact(data, &record); err != nil {
			manifest.Status = CampaignInvalid
			return manifest, err
		}
		wantHash := record.RecordHash
		record.RecordHash = ""
		canonical, _ := json.Marshal(record)
		spec := contract.Checkpoints[ordinal-1]
		epochOK := record.EpochSequence >= 1 && record.EpochSequence <= len(arms) &&
			arms[record.EpochSequence-1].ID == record.EpochID && arms[record.EpochSequence-1].BootID == record.BootID
		if wantHash == "" || digest(canonical) != wantHash || record.SchemaVersion != SchemaVersion || record.RunID != contract.RunID ||
			record.Scenario != spec.Scenario || record.Checkpoint != spec.ID || !epochOK {
			manifest.Status = CampaignInvalid
			return manifest, fmt.Errorf("checkpoint authority or integrity mismatch: %s", entry.Name())
		}
		record.RecordHash = wantHash
		if err := normalizeCheckpointRecord(&record); err != nil || record.Redaction != "PASS" || record.SecretScan != "PASS" {
			manifest.Status = CampaignInvalid
			return manifest, errors.Join(fmt.Errorf("checkpoint record is invalid: %s", entry.Name()), err)
		}
		if record.Outcome == CheckpointFail {
			evidenceSHA256, err := failureEvidenceDigest(root, record.FailureSequence)
			if err != nil || record.EvidenceSHA256 != evidenceSHA256 {
				manifest.Status = CampaignInvalid
				return manifest, errors.Join(fmt.Errorf("failure checkpoint evidence digest is invalid: %s", entry.Name()), err)
			}
		}
		records[ordinal] = record
		manifest.CheckpointRecords = append(manifest.CheckpointRecords, CheckpointSeal{Scenario: record.Scenario,
			Checkpoint: record.Checkpoint, Outcome: record.Outcome,
			Filename: filepath.ToSlash(filepath.Join(checkpointsDirectory, entry.Name())), SHA256: digest(data)})
	}
	sort.Slice(manifest.CheckpointRecords, func(i, j int) bool {
		return checkpointIndex(contract, manifest.CheckpointRecords[i].Scenario, manifest.CheckpointRecords[i].Checkpoint) <
			checkpointIndex(contract, manifest.CheckpointRecords[j].Scenario, manifest.CheckpointRecords[j].Checkpoint)
	})
	manifest.CheckpointCount = len(records)

	failureEntries, err := os.ReadDir(filepath.Join(root, recordsDirectory))
	if err != nil {
		manifest.Status = CampaignInvalid
		return manifest, err
	}
	maxFailure := 0
	for _, entry := range failureEntries {
		match := recordFilenamePattern.FindStringSubmatch(entry.Name())
		if entry.IsDir() || len(match) != 3 || !containsGroup(Group(match[2])) {
			manifest.Status = CampaignInvalid
			return manifest, fmt.Errorf("unexpected failure record %q", entry.Name())
		}
		sequence, parseErr := strconv.Atoi(match[1])
		if parseErr != nil || sequence < 1 || sequence > MaxFailures {
			manifest.Status = CampaignInvalid
			return manifest, fmt.Errorf("failure record sequence is invalid: %s", entry.Name())
		}
		if sequence > maxFailure {
			maxFailure = sequence
		}
	}
	manifest.FailureCount = maxFailure
	for sequence := 1; sequence <= maxFailure; sequence++ {
		var sequenceCheckpoint CheckpointRecord
		for _, checkpoint := range records {
			if checkpoint.Outcome == CheckpointFail && checkpoint.FailureSequence == sequence {
				sequenceCheckpoint = checkpoint
				break
			}
		}
		if sequenceCheckpoint.RunID == "" {
			manifest.Status = CampaignInvalid
			return manifest, errors.New("failure diagnostics have no terminal checkpoint")
		}
		for _, group := range groups {
			name := fmt.Sprintf("%02d-%s.json", sequence, group)
			data, err := readBounded(filepath.Join(root, recordsDirectory, name), MaxRecordBytes)
			if err != nil {
				manifest.Status = CampaignInvalid
				return manifest, errors.Join(fmt.Errorf("missing failure diagnostic %s", name), err)
			}
			var record Record
			if err := decodeExact(data, &record); err != nil {
				manifest.Status = CampaignInvalid
				return manifest, err
			}
			wantHash := record.RecordHash
			record.RecordHash = ""
			canonical, _ := json.Marshal(record)
			if wantHash == "" || digest(canonical) != wantHash || record.SchemaVersion != SchemaVersion || record.RunID != contract.RunID ||
				record.FailureSequence != sequence || record.Group != group || record.Scenario != sequenceCheckpoint.Scenario ||
				record.Checkpoint != sequenceCheckpoint.Checkpoint || record.EpochSequence != sequenceCheckpoint.EpochSequence ||
				record.EpochID != sequenceCheckpoint.EpochID || record.BootID != sequenceCheckpoint.BootID {
				manifest.Status = CampaignInvalid
				return manifest, fmt.Errorf("failure diagnostic authority or integrity mismatch: %s", name)
			}
			record.RecordHash = wantHash
			if record.Redaction != "PASS" {
				manifest.Redaction = "FAIL"
			}
			if record.SecretScan != "PASS" {
				manifest.SecretScan = "FAIL"
			}
			if err := normalizeRecord(&record); err != nil || record.State == StateCaptureError || record.State == StateUnstable ||
				record.Redaction != "PASS" || record.SecretScan != "PASS" {
				manifest.Status = CampaignInvalid
				return manifest, errors.Join(fmt.Errorf("failure diagnostic is invalid: %s", name), err)
			}
			manifest.Records = append(manifest.Records, RecordSeal{FailureSequence: sequence, Group: group,
				Filename: filepath.ToSlash(filepath.Join(recordsDirectory, name)), SHA256: digest(data)})
		}
	}
	if len(failureEntries) != maxFailure*len(groups) {
		manifest.Status = CampaignInvalid
		return manifest, errors.New("failure diagnostic cardinality differs from the campaign")
	}

	failedOrdinal := 0
	for ordinal, record := range records {
		if record.Outcome == CheckpointFail {
			if failedOrdinal != 0 {
				manifest.Status = CampaignInvalid
				return manifest, errors.New("campaign contains multiple terminal failures")
			}
			failedOrdinal = ordinal
		}
	}
	if failedOrdinal == 0 {
		if maxFailure != 0 || len(records) != len(contract.Checkpoints) {
			return manifest, errors.New("campaign is incomplete")
		}
		for ordinal := 1; ordinal <= len(contract.Checkpoints); ordinal++ {
			if records[ordinal].Outcome != CheckpointPass {
				return manifest, errors.New("campaign is incomplete")
			}
		}
		manifest.Status, manifest.Completeness = CampaignCompleteSuccess, "PASS"
		return manifest, nil
	}
	if maxFailure != 1 || records[failedOrdinal].FailureSequence != 1 {
		manifest.Status = CampaignInvalid
		return manifest, errors.New("terminal failure diagnostics are ambiguous")
	}
	for ordinal := 1; ordinal < failedOrdinal; ordinal++ {
		if records[ordinal].Outcome != CheckpointPass {
			return manifest, errors.New("campaign failure is missing a required successful prefix")
		}
	}
	for ordinal := failedOrdinal + 1; ordinal <= len(contract.Checkpoints); ordinal++ {
		if records[ordinal].RunID != "" {
			manifest.Status = CampaignInvalid
			return manifest, errors.New("campaign contains evidence after its terminal failure")
		}
	}
	manifest.Status, manifest.Completeness = CampaignCompleteFailure, "PASS"
	return manifest, nil
}

func rejectUnexpectedCampaignFiles(root string) error {
	expected := map[string]bool{armFilename: true, manifestFilename: true, recordsDirectory: true,
		checkpointsDirectory: true, epochsDirectory: true}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !expected[entry.Name()] {
			return fmt.Errorf("unexpected bundle entry %q", entry.Name())
		}
	}
	for _, directory := range []string{recordsDirectory, checkpointsDirectory, epochsDirectory} {
		if err := validatePrivateDirectory(filepath.Join(root, directory)); err != nil {
			return err
		}
	}
	return nil
}
