package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

type inputProbe struct {
	read  int
	calls int
	fail  bool
}

func (p *inputProbe) Read(buffer []byte) (int, error) {
	p.calls++
	if p.fail {
		return 0, errors.New("credential-sentinel")
	}
	for i := range buffer {
		buffer[i] = ' '
	}
	p.read += len(buffer)
	return len(buffer), nil
}

func TestArtifactObservationRejectsPathArgumentsBeforeReadingInput(t *testing.T) {
	for _, args := range [][]string{nil, {""}, {"credential-sentinel", "extra"}} {
		input := &inputProbe{fail: true}
		var output bytes.Buffer
		err := run(args, input, &output)
		if err == nil || strings.Contains(err.Error(), "credential-sentinel") || input.calls != 0 || output.Len() != 0 {
			t.Fatal("invalid invocation read input, echoed arguments or wrote observation")
		}
	}
}

func TestArtifactObservationBoundsAndStrictlyDecodesSource(t *testing.T) {
	for name, input := range map[string]io.Reader{
		"oversized": &inputProbe{}, "read-error": &inputProbe{fail: true},
		"duplicate": strings.NewReader(`{"id":"sing-box","\u0069d":"sing-box"}`),
		"unknown":   strings.NewReader(`{"credential-sentinel":true}`),
		"utf8":      strings.NewReader("{\"id\":\"\xff\"}"), "trailing": strings.NewReader(`{} {}`),
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			err := run([]string{"credential-sentinel"}, input, &output)
			if err == nil || strings.Contains(err.Error(), "credential-sentinel") || output.Len() != 0 {
				t.Fatal("invalid source accepted, echoed or emitted an observation")
			}
			if counted, ok := input.(*inputProbe); ok && counted.read > (64<<10)+1 {
				t.Fatal("source input exceeded its read bound")
			}
		})
	}
}

func TestArtifactObservationRejectsUnusableCandidatesWithoutExecuting(t *testing.T) {
	source, err := json.Marshal(providerfixture.Identity(t, "sing-box").Source)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	empty, invalid := filepath.Join(root, "empty"), filepath.Join(root, "credential-sentinel")
	for path, raw := range map[string][]byte{empty: {}, invalid: []byte("credential-sentinel")} {
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, path := range map[string]string{"missing": filepath.Join(root, "missing"), "directory": root, "empty": empty, "not-executable": invalid} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			err := run([]string{path}, bytes.NewReader(source), &output)
			if err == nil || strings.Contains(err.Error(), "credential-sentinel") || output.Len() != 0 {
				t.Fatal("unusable candidate accepted, echoed or emitted an observation")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		link := filepath.Join(root, "link")
		if err := os.Symlink(invalid, link); err != nil {
			t.Skip("symlink fixture unavailable on this host")
		}
		var output bytes.Buffer
		if err := run([]string{link}, bytes.NewReader(source), &output); err == nil || output.Len() != 0 {
			t.Fatal("symlink candidate accepted")
		}
	})
}
