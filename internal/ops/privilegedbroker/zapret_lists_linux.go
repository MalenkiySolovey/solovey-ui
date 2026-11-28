//go:build linux && !minimal

package privilegedbroker

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"golang.org/x/sys/unix"
)

// The broker unit's PrivateTmp keeps this generation's inputs out of other
// services' /tmp. A named file is required by nfqws2's realpath-based loader.
const zapretListDirPrefix = "/tmp/solovey-zapret-lists-"

var errZapretListStage = errors.New("the broker cannot stage the selected Zapret lists")
var errZapretListStart = errors.New("the pinned engine could not load the selected Zapret lists")

func stageZapretLists(command ZapretEngineCommand) (_ func() error, failure error) {
	if len(command.ListFiles) == 0 {
		return func() error { return nil }, nil
	}
	dir := command.listDir
	id, ok := strings.CutPrefix(dir, zapretListDirPrefix)
	if !ok || !zapretactivation.ValidDigest(id) {
		return nil, errZapretListStage
	}
	var quota unix.Statfs_t
	if err := unix.Statfs(filepath.Dir(dir), &quota); err != nil || quota.Bsize <= 0 {
		return nil, fmt.Errorf("%w: private workspace capacity is unavailable", errZapretListStage)
	}
	if quota.Files != 0 && uint64(len(command.ListFiles))+1 > quota.Ffree {
		return nil, fmt.Errorf("%w: private workspace has %d free file slots for %d lists", errZapretListStage, quota.Ffree, len(command.ListFiles))
	}
	blocks, blockSize := quota.Bavail, uint64(quota.Bsize)
	for _, text := range command.ListFiles {
		need := (uint64(len(text)) + blockSize - 1) / blockSize
		if need > blocks {
			return nil, fmt.Errorf("%w: insufficient filesystem space for the selected native list files", errZapretListStage)
		}
		blocks -= need
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, fmt.Errorf("%w: cannot create the generation directory", errZapretListStage)
	}
	created := 0
	listPath := func(i int) string { return filepath.Join(dir, "list-"+strconv.Itoa(i)) }
	cleanup := func() error {
		var result error
		for i := range created {
			result = errors.Join(result, os.Remove(listPath(i)))
		}
		return errors.Join(result, os.Remove(dir))
	}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, cleanup())
		}
	}()
	for i, text := range command.ListFiles {
		file, err := os.OpenFile(listPath(i), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400) // #nosec G304 -- generated path under a new broker-owned directory
		if err != nil {
			return nil, fmt.Errorf("%w: cannot create native list %d", errZapretListStage, i+1)
		}
		created++
		_, err = io.WriteString(file, text)
		if err == nil {
			err = file.Chown(0, zapretEngineUID)
		}
		if err == nil {
			err = file.Chmod(0o440)
		}
		if err := errors.Join(err, file.Close()); err != nil {
			return nil, fmt.Errorf("%w: cannot write or protect native list %d", errZapretListStage, i+1)
		}
	}
	if err := os.Chown(dir, 0, zapretEngineUID); err != nil {
		return nil, fmt.Errorf("%w: cannot set the native list directory owner", errZapretListStage)
	}
	if err := os.Chmod(dir, 0o710); err != nil {
		return nil, fmt.Errorf("%w: cannot grant the engine read-only native list access", errZapretListStage)
	}
	return cleanup, nil
}
