package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

const (
	backupSuffix          = ".bak"
	pendingSuffix         = ".update-pending"
	maxArtifactBytes      = 512 << 20
	maxChecksumBytes      = 4 << 10
	downloadTimeout       = 5 * time.Minute
	rollbackAfterAttempts = 2
)

var ErrChecksumMismatch = errors.New("artifact checksum does not match the published value")

type PipelineDeps struct {
	Client   HTTPDoer
	ExecPath string
}

func DefaultPipelineDeps() PipelineDeps {
	executable, err := os.Executable()
	if err != nil {
		executable = ""
	}
	return PipelineDeps{Client: &http.Client{Timeout: downloadTimeout}, ExecPath: executable}
}

func ApplyPipeline(target ReleaseTarget, deps PipelineDeps, setStage func(UpdateStage)) error {
	if deps.ExecPath == "" {
		return errors.New("cannot locate current executable")
	}
	if deps.Client == nil {
		return errors.New("update HTTP client is not configured")
	}
	archive := filepath.Join(filepath.Dir(deps.ExecPath), ".solovey-ui-update.tar.gz")
	defer os.Remove(archive)
	setStage(UpdateStageDownloading)
	if err := downloadToFile(deps.Client, target.AssetURL, archive); err != nil {
		return err
	}
	expected, err := downloadChecksum(deps.Client, target.ChecksumURL)
	if err != nil {
		return err
	}
	setStage(UpdateStageVerifying)
	if err := verifySHA256(archive, expected); err != nil {
		return err
	}
	setStage(UpdateStageApplying)
	return swapBinary(archive, deps.ExecPath)
}

func downloadToFile(client HTTPDoer, sourceURL, destination string) error {
	if !strings.HasPrefix(sourceURL, "https://") {
		return errors.New("refusing non-https artifact URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "solovey-ui-self-update")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("artifact download failed: status %d", response.StatusCode)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, maxArtifactBytes+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if written > maxArtifactBytes {
		return errors.New("artifact exceeds size limit")
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func downloadChecksum(client HTTPDoer, sourceURL string) (string, error) {
	if !strings.HasPrefix(sourceURL, "https://") {
		return "", errors.New("refusing non-https checksum URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "solovey-ui-self-update")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("checksum download failed: status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxChecksumBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxChecksumBytes {
		return "", errors.New("checksum response exceeds size limit")
	}
	fields := strings.Fields(string(body))
	if len(fields) == 0 {
		return "", errors.New("empty checksum file")
	}
	checksum := strings.ToLower(fields[0])
	decoded, err := hex.DecodeString(checksum)
	if err != nil || len(decoded) != sha256.Size {
		return "", errors.New("invalid SHA-256 checksum")
	}
	return checksum, nil
}

func verifySHA256(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expected) {
		return ErrChecksumMismatch
	}
	return nil
}

func swapBinary(archive, executable string) error {
	newBinary := executable + ".new"
	removeUpdateFileBestEffort(newBinary)
	if err := extractBinary(archive, newBinary); err != nil {
		removeUpdateFileBestEffort(newBinary)
		return err
	}
	if err := os.Chmod(newBinary, 0o755); err != nil {
		removeUpdateFileBestEffort(newBinary)
		return err
	}
	if err := copyFile(executable, executable+backupSuffix); err != nil {
		removeUpdateFileBestEffort(newBinary)
		return err
	}
	if runtime.GOOS == "windows" {
		if err := os.Remove(executable); err != nil {
			removeUpdateFileBestEffort(newBinary)
			return err
		}
	}
	if err := os.Rename(newBinary, executable); err != nil {
		removeUpdateFileBestEffort(newBinary)
		_ = RestoreBackup(executable)
		return err
	}
	return nil
}

func extractBinary(archive, destination string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("archive does not contain solovey-ui/solovey-ui")
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg || filepath.ToSlash(header.Name) != "solovey-ui/solovey-ui" {
			continue
		}
		if header.Size < 0 || header.Size > maxArtifactBytes {
			return errors.New("binary entry exceeds size limit")
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(output, io.LimitReader(tarReader, maxArtifactBytes+1))
		syncErr := output.Sync()
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if written > maxArtifactBytes || written != header.Size {
			return errors.New("invalid binary entry size")
		}
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func RestoreBackup(executable string) error {
	backup := executable + backupSuffix
	if _, err := os.Stat(backup); err != nil {
		return err
	}
	rollback := executable + ".rollback"
	_ = os.Remove(rollback)
	if err := copyFile(backup, rollback); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(executable)
	}
	if err := os.Rename(rollback, executable); err != nil {
		_ = os.Remove(rollback)
		return err
	}
	return os.Chmod(executable, 0o755)
}

func writePendingMarker(executable string) error {
	return os.WriteFile(executable+pendingSuffix, []byte("0"), 0o600)
}

func ClearPending(executable string) {
	_ = os.Remove(executable + pendingSuffix)
}

func CheckPending(executable string) bool {
	marker := executable + pendingSuffix
	raw, err := os.ReadFile(marker)
	if err != nil {
		return false
	}
	attempts, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		logger.Warning("panel update: invalid pending marker, resetting attempts:", err)
		attempts = 0
	}
	attempts++
	if attempts >= rollbackAfterAttempts {
		if err := RestoreBackup(executable); err == nil {
			_ = os.Remove(marker)
			return true
		} else {
			logger.Error("panel update: rollback backup unavailable after ", attempts, " boots: ", err)
		}
	}
	if err := os.WriteFile(marker, []byte(strconv.Itoa(attempts)), 0o600); err != nil {
		logger.Warning("panel update: pending marker update failed:", err)
	}
	return false
}

func removeUpdateFileBestEffort(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Warning("panel update: cleanup failed:", err)
	}
}
