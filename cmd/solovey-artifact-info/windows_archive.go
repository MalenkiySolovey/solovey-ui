package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

// Windows ZIPs are a distribution container, never an installed Update format.
// Observe all members without extraction or candidate execution. Windows names
// are case-insensitive, so aliases are rejected even on a Linux verifier.
func inspectWindowsArchive(input io.ReaderAt, size int64, source release.CoreProviderSource) (release.PanelBuildIdentity, []archiveMemberObservation, error) {
	invalid := errors.New("Windows package is malformed, ambiguous or exceeds bounds")
	reader, err := zip.NewReader(input, size)
	if err != nil || len(reader.File) == 0 || len(reader.File) > 64 {
		return release.PanelBuildIdentity{}, nil, invalid
	}
	seen := map[string]bool{}
	label := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	var total uint64
	var build release.PanelBuildIdentity
	var metadata bytes.Buffer
	var panel *os.File
	defer func() {
		if panel != nil {
			_ = panel.Close()
			_ = os.Remove(panel.Name())
		}
	}()
	members := make([]archiveMemberObservation, 0, len(reader.File))
	for _, member := range reader.File {
		if !strings.HasPrefix(member.Name, "solovey-ui-windows/") || seen[strings.ToLower(member.Name)] {
			return release.PanelBuildIdentity{}, nil, invalid
		}
		seen[strings.ToLower(member.Name)] = true
		relative := strings.TrimPrefix(member.Name, "solovey-ui-windows/")
		if relative == "" && member.FileInfo().IsDir() {
			continue
		}
		if !label.MatchString(relative) || !member.Mode().IsRegular() || member.UncompressedSize64 == 0 || member.UncompressedSize64 > 512<<20 {
			return release.PanelBuildIdentity{}, nil, invalid
		}
		total += member.UncompressedSize64
		if total > 1<<30 {
			return release.PanelBuildIdentity{}, nil, invalid
		}
		stream, openErr := member.Open()
		if openErr != nil {
			return release.PanelBuildIdentity{}, nil, invalid
		}
		hash := sha256.New()
		var destination io.Writer = hash
		if relative == "sui.exe" {
			panel, err = os.CreateTemp("", "solovey-artifact-windows-*")
			if err != nil {
				_ = stream.Close()
				return release.PanelBuildIdentity{}, nil, errors.New("private candidate observation storage is unavailable")
			}
			destination = io.MultiWriter(hash, panel)
		} else if relative == "BUILD_INFO.txt" {
			if member.UncompressedSize64 > 64<<10 {
				_ = stream.Close()
				return release.PanelBuildIdentity{}, nil, invalid
			}
			destination = io.MultiWriter(hash, &metadata)
		}
		written, copyErr := io.Copy(destination, io.LimitReader(stream, int64(member.UncompressedSize64)+1))
		closeErr := stream.Close()
		if copyErr != nil || closeErr != nil || uint64(written) != member.UncompressedSize64 {
			return release.PanelBuildIdentity{}, nil, invalid
		}
		if relative == "sui.exe" {
			info, infoErr := buildinfo.Read(panel)
			cgo := false
			if infoErr == nil {
				for _, setting := range info.Settings {
					if setting.Key == "CGO_ENABLED" && setting.Value == "1" {
						cgo = true
					}
				}
			}
			build, err = release.InspectPanelBuild(panel, source)
			if err != nil || !cgo || build.Platform != "windows" {
				return release.PanelBuildIdentity{}, nil, invalid
			}
		}
		members = append(members, archiveMemberObservation{relative, written, hex.EncodeToString(hash.Sum(nil))})
	}
	if panel == nil || !windowsBuildInfoMatches(metadata.String(), build) {
		return release.PanelBuildIdentity{}, nil, invalid
	}
	slices.SortFunc(members, func(a, b archiveMemberObservation) int { return strings.Compare(a.Name, b.Name) })
	return build, members, nil
}

func windowsBuildInfoMatches(raw string, build release.PanelBuildIdentity) bool {
	info := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if !ok || key == "" || info[key] != "" || value == "" {
			return false
		}
		info[key] = value
	}
	commit, commitErr := hex.DecodeString(info["commit"])
	fingerprint, fingerprintErr := hex.DecodeString(info["source_fingerprint"])
	return commitErr == nil && len(commit) == 20 && fingerprintErr == nil && len(fingerprint) == 32 &&
		info["app"] == "solovey-ui" && info["core_provider"] == build.CoreProvider.ID &&
		info["sing_box"] == "v"+build.CoreProvider.RuntimeSemanticVersion && info["profile"] == build.BinaryProfile &&
		info["platform"] == "windows/"+build.Arch
}
