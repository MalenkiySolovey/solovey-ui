package corecheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

type endlessInput struct{ bytes int }

func (r *endlessInput) Read(buffer []byte) (int, error) {
	for i := range buffer {
		buffer[i] = ' '
	}
	r.bytes += len(buffer)
	return len(buffer), nil
}

type failingIO struct{}

func (failingIO) Read([]byte) (int, error)  { return 0, errors.New("credential-sentinel-read") }
func (failingIO) Write([]byte) (int, error) { return 0, errors.New("credential-sentinel-write") }

func TestCoreCheckClosedArgumentsAndBoundedInput(t *testing.T) {
	for _, command := range []string{"core-info", "core-check", "run"} {
		input := &endlessInput{}
		var out, diagnostics bytes.Buffer
		if code := Run(command, []string{"--path", "/untrusted"}, input, &out, &diagnostics); code != 2 ||
			input.bytes != 0 || out.Len() != 0 || diagnostics.Len() == 0 {
			t.Fatal("invalid command authority reached input or validation")
		}
	}
	input := &endlessInput{}
	var out, diagnostics bytes.Buffer
	if code := Run("core-check", nil, input, &out, &diagnostics); code != 1 || input.bytes != validation.MaxTargetConfigBytes+1 {
		t.Fatalf("unbounded or successful over-limit input: %d bytes=%d", code, input.bytes)
	}
	var report validation.TargetCheck
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.State == "VALID" || diagnostics.Len() != 0 {
		t.Fatalf("invalid target report: %s %v", out.String(), err)
	}
}

func TestCoreInfoDoesNotReadInputAndKeepsArtifactTrustSeparate(t *testing.T) {
	input := &endlessInput{}
	var out, diagnostics bytes.Buffer
	code := Run("core-info", nil, input, &out, &diagnostics)
	var identity coreprovider.Identity
	if err := json.Unmarshal(out.Bytes(), &identity); err != nil || input.bytes != 0 || diagnostics.Len() != 0 ||
		identity.Revision != coreprovider.Current().Revision || identity.ArtifactState != coreprovider.Unknown || identity.ArtifactDigest != "" {
		t.Fatalf("invalid source-only info: %s %v", out.String(), err)
	}
	if (code == 0) != coreprovider.SourceIdentityVerified(identity) {
		t.Fatal("CLI success disagrees with source owner")
	}
}

func TestCoreCheckNeverEchoesInputOrIOFailures(t *testing.T) {
	for _, input := range []io.Reader{failingIO{}, strings.NewReader(`{"credential-sentinel-input":true}`)} {
		var out, diagnostics bytes.Buffer
		if code := Run("core-check", nil, input, &out, &diagnostics); code != 1 ||
			strings.Contains(out.String()+diagnostics.String(), "credential-sentinel") {
			t.Fatal("configuration or IO failure echoed")
		}
	}
	var diagnostics bytes.Buffer
	if code := Run("core-info", nil, nil, failingIO{}, &diagnostics); code != 1 || strings.Contains(diagnostics.String(), "credential-sentinel") {
		t.Fatal("output failure echoed")
	}
}
