//go:build linux

package deploymentidentity

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"
)

// LoadInstalled reads the fixed production contract without following a
// substituted final component. Parent ownership/mode and file ownership/mode
// are checked before any contract bytes are trusted.
func LoadInstalled() (ApplicationOwnerContractV1, error) { return LoadFromPath(InstalledContractPath) }

func LoadFromPath(name string) (ApplicationOwnerContractV1, error) {
	var value ApplicationOwnerContractV1
	if err := loadRootOwnedContract(name, &value); err != nil {
		return ApplicationOwnerContractV1{}, err
	}
	return value, value.Validate()
}

// LoadProcdInstalled reads the separate procd supervisor contract. It does
// not make the systemd loader accept a different schema.
func LoadProcdInstalled() (ApplicationOwnerContractProcdV1, error) {
	return LoadProcdFromPath(ProcdInstalledContractPath)
}

func LoadProcdFromPath(name string) (ApplicationOwnerContractProcdV1, error) {
	var value ApplicationOwnerContractProcdV1
	if err := loadRootOwnedContract(name, &value); err != nil {
		return ApplicationOwnerContractProcdV1{}, err
	}
	return value, value.Validate()
}

func loadRootOwnedContract(name string, target any) error {
	if !canonicalAbsolute(name) {
		return errors.New("application owner contract path is not canonical")
	}
	parentInfo, err := os.Lstat(path.Dir(name))
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("application owner contract parent is unsafe")
	}
	if stat, ok := parentInfo.Sys().(*syscall.Stat_t); !ok || stat.Uid != 0 || stat.Gid != 0 || parentInfo.Mode().Perm()&0o022 != 0 {
		return errors.New("application owner contract parent ownership is unsafe")
	}
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > MaxContractBytes {
		return errors.New("application owner contract file is unsafe")
	}
	before, ok := info.Sys().(*syscall.Stat_t)
	if !ok || before.Uid != 0 || before.Gid != 0 || info.Mode().Perm() != 0o444 {
		return errors.New("application owner contract ownership is unsafe")
	}
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, MaxContractBytes+1))
	afterInfo, statErr := file.Stat()
	closeErr := file.Close()
	if readErr != nil || statErr != nil || closeErr != nil || len(data) == 0 || len(data) > MaxContractBytes {
		return errors.New("application owner contract read failed")
	}
	var after *syscall.Stat_t
	afterOK := false
	if afterInfo != nil {
		after, afterOK = afterInfo.Sys().(*syscall.Stat_t)
	}
	if !afterOK || before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || before.Mtim != after.Mtim {
		return errors.New("application owner contract changed while reading")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode application owner contract: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("application owner contract contains multiple values")
	}
	return nil
}
