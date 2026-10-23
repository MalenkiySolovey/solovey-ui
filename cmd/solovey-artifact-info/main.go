// solovey-artifact-info is a local packaging tool. It observes an operator's
// candidate file without executing it and grants no installation authority.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader, output io.Writer) error {
	if len(args) != 1 || args[0] == "" {
		return errors.New("usage: solovey-artifact-info BUILT_PANEL; expected source JSON on stdin")
	}
	raw, err := io.ReadAll(io.LimitReader(input, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 || jsonvalue.StrictObject(raw) != nil {
		return errors.New("expected source JSON is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var source release.CoreProviderSource
	if decoder.Decode(&source) != nil || release.ValidateCoreProviderSource(source) != nil {
		return errors.New("expected source declaration is invalid")
	}
	info, err := os.Lstat(args[0])
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 512<<20 {
		return errors.New("candidate is not a bounded regular file")
	}
	// #nosec G304 -- explicit operator input for read-only packaging observation, never a Panel API or execution path.
	file, err := os.Open(args[0])
	if err != nil {
		return errors.New("candidate file could not be opened")
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("candidate file changed before observation")
	}
	metadata, err := release.InspectPanelBuild(file, source)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.NewSectionReader(file, 0, info.Size()))
	if err != nil || written != info.Size() {
		return errors.New("candidate content digest is unavailable")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || after.Size() != info.Size() || after.ModTime() != info.ModTime() {
		return errors.New("candidate changed during observation")
	}
	if err := json.NewEncoder(output).Encode(struct {
		Schema        string                     `json:"schema"`
		ContentDigest string                     `json:"contentDigest"`
		Size          int64                      `json:"size"`
		Build         release.PanelBuildIdentity `json:"build"`
	}{"solovey.panel-artifact-observation/v1", hex.EncodeToString(hash.Sum(nil)), written, metadata}); err != nil {
		return errors.New("candidate observation could not be written")
	}
	return nil
}
