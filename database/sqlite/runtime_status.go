package sqlite

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

const (
	SQLiteModuleVersion   = "v1.14.49"
	SQLiteModuleCommit    = "cc41b8c87686036ea632cede537ffccef69b370a"
	SQLiteModuleSum       = "h1:B8jBHC3xhxZgxztrgruTuLucebnULQnx4W7cF7SAE9w="
	SQLiteRuntimeVersion  = "3.53.4"
	SQLiteRuntimeSourceID = "2026-07-24 19:02:57 bf7c7f30031888f4e796e429ab3978879485813aaca6f641c7b33e4e09459bcc"
	WALResetSafetyClass   = "fixed-upstream-3.51.3-or-later"
)

type RuntimeStatus struct {
	Provider          string   `json:"provider"`
	ModuleVersion     string   `json:"moduleVersion"`
	ModuleCommit      string   `json:"moduleCommit"`
	ModuleSum         string   `json:"moduleSum"`
	RuntimeVersion    string   `json:"runtimeVersion"`
	RuntimeVersionNum int      `json:"runtimeVersionNumber"`
	SourceID          string   `json:"sourceId"`
	CompileOptions    []string `json:"compileOptions"`
	JournalMode       string   `json:"journalMode"`
	WALCapable        bool     `json:"walCapable"`
	WALResetSafe      bool     `json:"walResetSafe"`
	RuntimePinned     bool     `json:"runtimePinned"`
	SafetyClass       string   `json:"safetyClass"`
	Revision          string   `json:"revision"`
}

func InspectRuntime(db *gorm.DB) (RuntimeStatus, error) {
	version, number, sourceID := sqlite3.Version()
	status := RuntimeStatus{Provider: "mattn-go-sqlite3", ModuleVersion: SQLiteModuleVersion,
		ModuleCommit: SQLiteModuleCommit, ModuleSum: SQLiteModuleSum, RuntimeVersion: version,
		RuntimeVersionNum: number, SourceID: sourceID, WALResetSafe: walResetSafe(number),
		RuntimePinned: version == SQLiteRuntimeVersion && sourceID == SQLiteRuntimeSourceID, SafetyClass: WALResetSafetyClass}
	if db == nil {
		status.Revision = runtimeStatusRevision(status)
		return status, errors.New("sqlite runtime database is unavailable")
	}
	if err := db.Raw("PRAGMA compile_options").Scan(&status.CompileOptions).Error; err != nil {
		return RuntimeStatus{}, err
	}
	sort.Strings(status.CompileOptions)
	if err := db.Raw("PRAGMA journal_mode").Scan(&status.JournalMode).Error; err != nil {
		return RuntimeStatus{}, err
	}
	status.JournalMode = strings.ToLower(strings.TrimSpace(status.JournalMode))
	status.WALCapable = status.JournalMode == "wal" && !hasCompileOption(status.CompileOptions, "OMIT_WAL")
	status.Revision = runtimeStatusRevision(status)
	return status, nil
}

func walResetSafe(versionNumber int) bool {
	// SQLite encodes X.Y.Z as X*1,000,000 + Y*1,000 + Z. The upstream fix is
	// in 3.51.3 and later; maintained branches 3.50.7 and 3.44.6 also carry it.
	if versionNumber >= 3_051_003 {
		return true
	}
	major := versionNumber / 1_000_000
	minor := (versionNumber / 1_000) % 1_000
	patch := versionNumber % 1_000
	return major == 3 && (minor == 50 && patch >= 7 || minor == 44 && patch >= 6)
}

func runtimeStatusRevision(status RuntimeStatus) string {
	status.Revision = ""
	data, _ := json.Marshal(status)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hasCompileOption(options []string, name string) bool {
	for _, option := range options {
		if option == name || strings.HasPrefix(option, name+"=") {
			return true
		}
	}
	return false
}

func ParseSQLiteVersion(value string) int {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return 0
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	patch, errPatch := strconv.Atoi(parts[2])
	if errMajor != nil || errMinor != nil || errPatch != nil || major < 0 || minor < 0 || patch < 0 || minor > 999 || patch > 999 {
		return 0
	}
	return major*1_000_000 + minor*1_000 + patch
}
