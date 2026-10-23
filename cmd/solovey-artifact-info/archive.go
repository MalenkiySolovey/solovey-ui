package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

type archiveMemberObservation struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// inspectArchive shares the extraction owner's portable container grammar.
// Only the Panel member is spooled to an exclusive private file for ReaderAt;
// all members are bounded and hashed, and no candidate is ever executed.
func inspectArchive(input io.Reader, source release.CoreProviderSource) (release.PanelBuildIdentity, []archiveMemberObservation, error) {
	reader, err := release.NewPanelArchiveReader(input)
	if err != nil {
		return release.PanelBuildIdentity{}, nil, err
	}
	defer func() { _ = reader.Close() }()
	var panel *os.File
	defer func() {
		if panel != nil {
			_ = panel.Close()
			_ = os.Remove(panel.Name())
		}
	}()
	var build release.PanelBuildIdentity
	members := make([]archiveMemberObservation, 0, 16)
	for {
		member, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return release.PanelBuildIdentity{}, nil, nextErr
		}
		hash := sha256.New()
		var destination io.Writer = hash
		if member.Relative == "solovey-ui" {
			panel, err = os.CreateTemp("", "solovey-artifact-panel-*")
			if err != nil {
				return release.PanelBuildIdentity{}, nil, errors.New("private candidate observation storage is unavailable")
			}
			destination = io.MultiWriter(hash, panel)
		}
		written, copyErr := io.Copy(destination, reader)
		if copyErr != nil || written != member.Size {
			return release.PanelBuildIdentity{}, nil, release.ErrPanelArchive
		}
		if member.Relative == "solovey-ui" {
			build, err = release.InspectPanelBuild(panel, source)
			if err != nil {
				return release.PanelBuildIdentity{}, nil, err
			}
		}
		members = append(members, archiveMemberObservation{member.Relative, written, hex.EncodeToString(hash.Sum(nil))})
	}
	if panel == nil {
		return release.PanelBuildIdentity{}, nil, errors.New("panel executable member is missing")
	}
	slices.SortFunc(members, func(a, b archiveMemberObservation) int { return strings.Compare(a.Name, b.Name) })
	return build, members, nil
}
