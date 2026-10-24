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
	if len(args) == 1 && args[0] == "--canonical-manifest" {
		return canonicalManifest(input, output)
	}
	if len(args) == 2 && args[0] == "--verify-release" && args[1] != "" {
		return verifyRelease(args[1], input, output)
	}
	archive := len(args) == 2 && args[0] == "--archive"
	if (len(args) != 1 && !archive) || args[len(args)-1] == "" || (len(args) == 1 && args[0][0] == '-') {
		return errors.New("usage: solovey-artifact-info [--archive] CANDIDATE; --canonical-manifest; --verify-release ENVELOPE; typed JSON on stdin")
	}
	candidate := args[len(args)-1]
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
	info, err := os.Lstat(candidate)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 512<<20 {
		return errors.New("candidate is not a bounded regular file")
	}
	// #nosec G304 -- explicit operator input for read-only packaging observation, never a Panel API or execution path.
	file, err := os.Open(candidate)
	if err != nil {
		return errors.New("candidate file could not be opened")
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("candidate file changed before observation")
	}
	var metadata release.PanelBuildIdentity
	var members []archiveMemberObservation
	if archive {
		metadata, members, err = inspectArchive(file, source)
	} else {
		metadata, err = release.InspectPanelBuild(file, source)
	}
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
	observation := struct {
		Schema        string                     `json:"schema"`
		ContentDigest string                     `json:"contentDigest"`
		Size          int64                      `json:"size"`
		Build         release.PanelBuildIdentity `json:"build"`
		Members       []archiveMemberObservation `json:"members,omitempty"`
	}{"solovey.panel-artifact-observation/v1", hex.EncodeToString(hash.Sum(nil)), written, metadata, members}
	if archive {
		observation.Schema = "solovey.panel-archive-observation/v1"
	}
	if err := json.NewEncoder(output).Encode(observation); err != nil {
		return errors.New("candidate observation could not be written")
	}
	return nil
}
