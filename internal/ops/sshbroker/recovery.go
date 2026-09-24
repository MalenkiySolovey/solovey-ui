package sshbroker

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

const (
	maxRecoveryEvidenceBytes   = 256 << 10
	maxRecoveryEvidenceRecords = 512
	maxRecoveryLogRecords      = 256
)

var (
	recoveryAcceptedLogin = regexp.MustCompile(`^Accepted (publickey|password|keyboard-interactive) for ([A-Za-z0-9._@-]{1,128}) from ([^ ]{1,128}) port ([0-9]{1,5}) ssh2(?:[: ].*)?$`)
	dropbearLogPattern    = regexp.MustCompile(`\[([0-9]{1,12})\.([0-9]{3})\] daemon\.(?:notice|info) dropbear\[([0-9]{1,10})\]: Pubkey auth succeeded for '([A-Za-z0-9._@-]{1,128})' with [^ ]+ key [^ ]+ from (\[[0-9A-Fa-f:]+\]:[0-9]{1,5}|[0-9.]+:[0-9]{1,5})$`)
)

// RecoveryObserver is the sole bounded owner of fresh SSH recovery evidence.
// Consumers receive semantic facts and revisions, never executable, unit,
// configuration, service-control, or log-backend selectors.
type RecoveryObserver interface {
	Detect(context.Context) RecoverySupport
	Observe(context.Context, RecoveryObserveRequest) (*RecoveryResult, error)
}

type RecoveryObserveRequest struct {
	SinceUnixMicros int64 `json:"sinceUnixMicros"`
	MaxEvents       int   `json:"maxEvents"`
}

type RecoverySupport struct {
	PlatformKnown    bool        `json:"platformKnown"`
	Linux            bool        `json:"linux"`
	Available        bool        `json:"available"`
	Reason           string      `json:"reason,omitempty"`
	EvidenceKind     LogEvidence `json:"evidenceKind,omitempty"`
	VerifierRevision string      `json:"verifierRevision,omitempty"`
	ObserverRevision string      `json:"observerRevision"`
}

type RecoveryObservation struct {
	ObservationID       string `json:"observationId"`
	PrincipalID         string `json:"principalId"`
	SourcePrefix        string `json:"sourcePrefix"`
	AuthenticationClass string `json:"authenticationClass"`
	ObservedAt          int64  `json:"observedAt"`
	ObservedAtMicros    int64  `json:"observedAtMicros"`
}

type RecoveryResult struct {
	VerifierRevision string                `json:"verifierRevision"`
	ObserverRevision string                `json:"observerRevision"`
	Observations     []RecoveryObservation `json:"observations"`
}

func RecoveryObserverRevision() string {
	return domain.Revision("sshbroker-bounded-fresh-recovery-observer/v1")
}

type unavailableRecoveryObserver struct {
	platformKnown bool
	linux         bool
	reason        string
}

func (u unavailableRecoveryObserver) Detect(context.Context) RecoverySupport {
	return RecoverySupport{PlatformKnown: u.platformKnown, Linux: u.linux, Reason: u.reason, ObserverRevision: RecoveryObserverRevision()}
}

func (u unavailableRecoveryObserver) Observe(context.Context, RecoveryObserveRequest) (*RecoveryResult, error) {
	return nil, errors.New(u.reason)
}

func validateRecoveryObserveRequest(request RecoveryObserveRequest, now time.Time) error {
	nowMicros := now.UTC().UnixMicro()
	if request.MaxEvents < 1 || request.MaxEvents > 64 || request.SinceUnixMicros <= 0 ||
		request.SinceUnixMicros > nowMicros+int64((5*time.Minute)/time.Microsecond) ||
		request.SinceUnixMicros < nowMicros-int64((10*time.Minute)/time.Microsecond) {
		return errors.New("SSH recovery observation window is invalid")
	}
	return nil
}

func parseOpenSSHRecoveryJournal(data []byte, request RecoveryObserveRequest, now time.Time, expectedExecutable string, expectedUnits []string) ([]RecoveryObservation, error) {
	if expectedExecutable == "" || (logEvidenceTarget{journaldUnits: expectedUnits}).validateFor(LogEvidenceJournald) != nil ||
		len(data) == 0 || len(data) > maxRecoveryEvidenceBytes || bytes.IndexByte(data, 0) >= 0 || validateRecoveryObserveRequest(request, now) != nil {
		return nil, errors.New("OpenSSH recovery evidence is malformed or unbounded")
	}
	result := make([]RecoveryObservation, 0)
	seen := make(map[string]struct{})
	records := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64<<10), maxRecoveryEvidenceBytes)
	for scanner.Scan() {
		records++
		if records > maxRecoveryEvidenceRecords || len(scanner.Bytes()) > 64<<10 {
			return nil, errors.New("OpenSSH recovery record bound was exceeded")
		}
		var row struct {
			Message    string `json:"MESSAGE"`
			Unit       string `json:"_SYSTEMD_UNIT"`
			Identifier string `json:"SYSLOG_IDENTIFIER"`
			Executable string `json:"_EXE"`
			Realtime   string `json:"__REALTIME_TIMESTAMP"`
			Cursor     string `json:"__CURSOR"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, errors.New("OpenSSH recovery journal returned malformed JSON")
		}
		if row.Identifier != "sshd" || row.Executable != expectedExecutable || !containsRecoveryUnit(expectedUnits, row.Unit) {
			continue
		}
		match := recoveryAcceptedLogin.FindStringSubmatch(row.Message)
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
		observation := RecoveryObservation{ObservationID: "recovery:" + hex.EncodeToString(observationSum[:]),
			PrincipalID: "principal:" + hex.EncodeToString(principalSum[:]), SourcePrefix: netip.PrefixFrom(address, bits).String(),
			AuthenticationClass: match[1], ObservedAt: micros / 1_000_000, ObservedAtMicros: micros}
		if _, duplicate := seen[observation.ObservationID]; duplicate {
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

func parseDropbearRecoveryLog(data []byte, request RecoveryObserveRequest, now time.Time, processMatches func(int) bool) ([]RecoveryObservation, error) {
	if len(data) == 0 || len(data) > maxRecoveryEvidenceBytes || bytes.IndexByte(data, 0) >= 0 || processMatches == nil || validateRecoveryObserveRequest(request, now) != nil {
		return nil, errors.New("dropbear recovery evidence is malformed or unbounded")
	}
	result := make([]RecoveryObservation, 0)
	seen := map[string]bool{}
	records := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64<<10), maxRecoveryEvidenceBytes)
	for scanner.Scan() {
		records++
		if records > maxRecoveryLogRecords || len(scanner.Bytes()) > 4096 {
			return nil, errors.New("dropbear recovery record bound was exceeded")
		}
		match := dropbearLogPattern.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		pid, _ := strconv.Atoi(match[3])
		remote, err := netip.ParseAddrPort(match[5])
		seconds, _ := strconv.ParseInt(match[1], 10, 64)
		millis, _ := strconv.ParseInt(match[2], 10, 64)
		micros := seconds*1_000_000 + millis*1_000
		if err != nil || pid <= 1 || !processMatches(pid) || remote.Addr().IsUnspecified() || remote.Addr().IsMulticast() ||
			micros <= request.SinceUnixMicros || micros > now.Add(5*time.Minute).UnixMicro() {
			continue
		}
		address := remote.Addr().Unmap()
		bits := 128
		if address.Is4() {
			bits = 32
		}
		principalSum := sha256.Sum256([]byte("ssh:" + match[4]))
		observationSum := sha256.Sum256([]byte(strconv.Itoa(pid) + "\x00" + strconv.FormatInt(micros, 10) + "\x00" + match[4] + "\x00" + address.String()))
		observation := RecoveryObservation{ObservationID: "recovery:" + hex.EncodeToString(observationSum[:]),
			PrincipalID: "principal:" + hex.EncodeToString(principalSum[:]), SourcePrefix: netip.PrefixFrom(address, bits).String(),
			AuthenticationClass: "publickey", ObservedAt: micros / 1_000_000, ObservedAtMicros: micros}
		if !seen[observation.ObservationID] {
			seen[observation.ObservationID] = true
			result = append(result, observation)
		}
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

func recoveryPublicKeyVerifierConfigurationProven(effective string) bool {
	effective = strings.ToLower(effective)
	return strings.Contains(effective, "pubkeyauthentication yes\n") &&
		strings.Contains(effective, "authorizedkeyscommand none\n") &&
		strings.Contains(effective, "strictmodes yes\n")
}

func containsRecoveryUnit(units []string, candidate string) bool {
	for _, unit := range units {
		if unit == candidate {
			return true
		}
	}
	return false
}
