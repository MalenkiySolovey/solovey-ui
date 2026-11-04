//go:build !minimal

package remote

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrSourceFilePathInvalid = errors.New("SUBSCRIPTION_SOURCE_FILE_PATH_INVALID")
var ErrSourceFileRead = errors.New("SUBSCRIPTION_SOURCE_FILE_READ_FAILED")

// A host file is an explicit local source. It does not participate in URL
// fetching, and a save never creates, replaces or removes the selected file.
func ValidateSourceFilePath(path string) error {
	if len(path) == 0 || len(path) > 4096 || strings.ContainsAny(path, "\x00\r\n") || !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return ErrSourceFilePathInvalid
	}
	return nil
}

func fetchHostFileSource(ctx context.Context, path string, options FetchOptions) (*FetchedSubscription, error) {
	if err := ValidateSourceFilePath(path); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Reject special files before opening: a FIFO/device is not a bounded
	// subscription source. Check the opened file again before reading it.
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > MaxInlineSourceBytes {
		return nil, ErrSourceFileRead
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrSourceFileRead
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() == 0 || before.Size() > MaxInlineSourceBytes {
		return nil, ErrSourceFileRead
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxInlineSourceBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > MaxInlineSourceBytes {
		return nil, ErrSourceFileRead
	}
	after, err := file.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || int64(len(raw)) != after.Size() {
		return nil, ErrSourceFileRead
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fetched, err := ParseFetchedSubscriptionWithOptions(string(raw), options)
	if err != nil {
		// Parser errors may quote source data. File contents, paths and native
		// OS errors must not disclose credentials through refresh status.
		return nil, ErrSourceContentInvalid
	}
	return fetched, nil
}
