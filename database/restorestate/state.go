// Package restorestate owns the bounded file-swap journal used to recover an
// interrupted database restore before migrations or SQLite bootstrap run.
package restorestate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	SchemaV1                = "solovey.restore-swap/v1"
	StateStaged             = "STAGED"
	StateLiveMovePending    = "LIVE_MOVE_PENDING"
	StateCandidatePending   = "CANDIDATE_INSTALL_PENDING"
	StateCommitted          = "COMMITTED"
	MaxMarkerBytes          = 4096
	MaxCandidateDigestBytes = int64(512 << 20)
)

type Marker struct {
	Schema          string `json:"schema"`
	State           string `json:"state"`
	CandidateDigest string `json:"candidateDigest"`
}

func MarkerPath(databasePath string) string   { return databasePath + ".restore-state.json" }
func StagingPath(databasePath string) string  { return databasePath + ".temp" }
func FallbackPath(databasePath string) string { return databasePath + ".backup" }

func EnsureIdle(databasePath string) error {
	if err := validateDatabasePath(databasePath); err != nil {
		return err
	}
	if err := ensureNoAuthority(databasePath); err != nil {
		return err
	}
	return removeDatabaseFile(StagingPath(databasePath))
}

func ensureNoAuthority(databasePath string) error {
	if _, err := os.Lstat(MarkerPath(databasePath)); err == nil {
		return errors.New("database restore recovery is required before a new restore")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Lstat(FallbackPath(databasePath)); err == nil {
		return errors.New("database restore fallback exists without a recovery marker")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func Begin(databasePath, candidateDigest string) error {
	if err := validateDatabasePath(databasePath); err != nil {
		return err
	}
	if err := ensureNoAuthority(databasePath); err != nil {
		return err
	}
	if !validDigest(candidateDigest) {
		return errors.New("database restore candidate digest is invalid")
	}
	if err := requireRegular(databasePath); err != nil {
		return errors.New("live database is unavailable for restore")
	}
	if err := requireRegular(StagingPath(databasePath)); err != nil {
		return errors.New("staged restore database is unavailable")
	}
	actual, err := fileDigest(StagingPath(databasePath), MaxCandidateDigestBytes)
	if err != nil || actual != candidateDigest {
		return errors.New("staged restore database digest changed")
	}
	return writeMarker(databasePath, Marker{Schema: SchemaV1, State: StateStaged, CandidateDigest: candidateDigest})
}

func Transition(databasePath, expected, next string) error {
	if !validTransition(expected, next) {
		return errors.New("database restore transition is invalid")
	}
	marker, err := readMarker(databasePath)
	if err != nil {
		return err
	}
	if marker.State != expected {
		return errors.New("database restore transition revision changed")
	}
	marker.State = next
	return writeMarker(databasePath, marker)
}

// MarkCommitted is the single acceptance point for an installed candidate.
// Callers must persist every authority that must survive with the candidate
// before crossing this boundary.
func MarkCommitted(databasePath string) error {
	return Transition(databasePath, StateCandidatePending, StateCommitted)
}

// FinalizeCommitted removes the exact fallback only after the candidate and
// its durable operation authority have both been accepted.
func FinalizeCommitted(databasePath string) error {
	marker, err := readMarker(databasePath)
	if err != nil {
		return err
	}
	if marker.State != StateCommitted {
		return errors.New("database restore is not committed")
	}
	if err := requireRegular(databasePath); err != nil {
		return errors.New("committed restore database is unavailable")
	}
	if err := removeDatabaseFile(FallbackPath(databasePath)); err != nil {
		return err
	}
	if err := removeDatabaseFile(StagingPath(databasePath)); err != nil {
		return err
	}
	return os.Remove(MarkerPath(databasePath))
}

func CancelStaged(databasePath string) error {
	marker, err := readMarker(databasePath)
	if err != nil {
		return err
	}
	if marker.State != StateStaged {
		return errors.New("database restore is no longer staged")
	}
	if err := removeDatabaseFile(StagingPath(databasePath)); err != nil {
		return err
	}
	return os.Remove(MarkerPath(databasePath))
}

func Rollback(databasePath string) error {
	marker, err := readMarker(databasePath)
	if err != nil {
		return err
	}
	if marker.State != StateLiveMovePending && marker.State != StateCandidatePending {
		return errors.New("database restore rollback state is invalid")
	}
	return rollbackToFallback(databasePath, marker)
}

// Recover reconciles the marker before migrations. Any non-committed swap is
// rolled back; a committed swap keeps the candidate and removes stale files.
func Recover(databasePath string) error {
	if err := validateDatabasePath(databasePath); err != nil {
		return err
	}
	marker, err := readMarker(databasePath)
	if errors.Is(err, os.ErrNotExist) {
		if _, fallbackErr := os.Lstat(FallbackPath(databasePath)); fallbackErr == nil {
			return errors.New("database restore fallback exists without a recovery marker")
		} else if !errors.Is(fallbackErr, os.ErrNotExist) {
			return fallbackErr
		}
		return removeDatabaseFile(StagingPath(databasePath))
	}
	if err != nil {
		return err
	}
	switch marker.State {
	case StateStaged:
		if err := requireRegular(databasePath); err != nil {
			return errors.New("staged restore marker exists but the live database is unavailable")
		}
		if _, fallbackErr := os.Lstat(FallbackPath(databasePath)); fallbackErr == nil {
			return errors.New("staged restore marker conflicts with a fallback database")
		} else if !errors.Is(fallbackErr, os.ErrNotExist) {
			return fallbackErr
		}
		return CancelStaged(databasePath)
	case StateLiveMovePending:
		if _, fallbackErr := os.Lstat(FallbackPath(databasePath)); errors.Is(fallbackErr, os.ErrNotExist) {
			if err := requireRegular(databasePath); err != nil {
				return errors.New("live-move restore marker has neither live nor fallback database")
			}
			return cancelIntent(databasePath)
		} else if fallbackErr != nil {
			return fallbackErr
		}
		return rollbackToFallback(databasePath, marker)
	case StateCandidatePending:
		return rollbackToFallback(databasePath, marker)
	case StateCommitted:
		return FinalizeCommitted(databasePath)
	default:
		return errors.New("database restore marker state is unsupported")
	}
}

func rollbackToFallback(databasePath string, marker Marker) error {
	fallback := FallbackPath(databasePath)
	if err := requireRegular(fallback); err != nil {
		return errors.New("database restore fallback is unavailable")
	}
	if marker.State == StateLiveMovePending {
		if _, err := os.Lstat(databasePath); err == nil {
			actual, digestErr := fileDigest(databasePath, MaxCandidateDigestBytes)
			if digestErr != nil || actual != marker.CandidateDigest {
				return errors.New("unexpected live database appeared during restore recovery")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := removeDatabaseFile(databasePath); err != nil {
		return err
	}
	if err := os.Rename(fallback, databasePath); err != nil {
		return err
	}
	if err := removeDatabaseFile(StagingPath(databasePath)); err != nil {
		return err
	}
	return os.Remove(MarkerPath(databasePath))
}

func cancelIntent(databasePath string) error {
	if err := removeDatabaseFile(StagingPath(databasePath)); err != nil {
		return err
	}
	return os.Remove(MarkerPath(databasePath))
}

func readMarker(databasePath string) (Marker, error) {
	var marker Marker
	path := MarkerPath(databasePath)
	info, err := os.Lstat(path)
	if err != nil {
		return marker, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxMarkerBytes {
		return marker, errors.New("database restore marker file is invalid")
	}
	data, err := os.ReadFile(path) // #nosec G304 -- fixed internal marker path.
	if err != nil {
		return marker, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return Marker{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Marker{}, errors.New("database restore marker contains multiple JSON values")
	}
	if marker.Schema != SchemaV1 || !validState(marker.State) || !validDigest(marker.CandidateDigest) {
		return Marker{}, errors.New("database restore marker identity is invalid")
	}
	return marker, nil
}

func writeMarker(databasePath string, marker Marker) error {
	if marker.Schema != SchemaV1 || !validState(marker.State) || !validDigest(marker.CandidateDigest) {
		return errors.New("database restore marker is invalid")
	}
	data, err := json.Marshal(marker)
	if err != nil || len(data)+1 > MaxMarkerBytes {
		return errors.New("database restore marker exceeds bounds")
	}
	data = append(data, '\n')
	path := MarkerPath(databasePath)
	temporary := path + ".partial"
	_ = os.Remove(temporary)
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func fileDigest(path string, limit int64) (string, error) {
	file, err := os.Open(path) // #nosec G304 -- fixed restore path.
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, limit+1))
	if err != nil || written <= 0 || written > limit {
		return "", errors.New("database restore file is outside bounds")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func removeDatabaseFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func requireRegular(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("restore database path is not a regular file")
	}
	return nil
}

func validateDatabasePath(path string) error {
	if path == "" || strings.Contains(path, "\x00") || strings.Contains(path, "?") ||
		filepath.Clean(path) != path || filepath.Base(path) == "." {
		return errors.New("database restore path is invalid")
	}
	return nil
}

func validTransition(expected, next string) bool {
	return expected == StateStaged && next == StateLiveMovePending ||
		expected == StateLiveMovePending && next == StateCandidatePending ||
		expected == StateCandidatePending && next == StateCommitted
}

func validState(value string) bool {
	return value == StateStaged || value == StateLiveMovePending || value == StateCandidatePending || value == StateCommitted
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
