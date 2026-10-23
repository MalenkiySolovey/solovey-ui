// This executable probes a built Panel's maintenance-only commands. It never
// starts the application or accepts a path through a Panel API.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 3 || os.Args[2] != "sing-box" && os.Args[2] != "shtorm-extended" {
		return errors.New("usage: core-target-check BUILT_PANEL sing-box|shtorm-extended")
	}
	binary, err := filepath.Abs(os.Args[1])
	if err != nil {
		return errors.New("probe executable path unavailable")
	}
	root, err := os.MkdirTemp("", "solovey-core-target-check-")
	if err != nil {
		return errors.New("probe scratch directory unavailable")
	}
	defer func() { _ = os.RemoveAll(root) }()

	invoke := func(input []byte, args ...string) ([]byte, []byte, int) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// #nosec G204 -- local qualification harness, caller supplies its built binary; no API input or shell.
		command := exec.CommandContext(ctx, binary, args...)
		command.Dir, command.Stdin = root, bytes.NewReader(input)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		code := 0
		if err != nil {
			code = -1
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				code = exit.ExitCode()
			}
		}
		return stdout.Bytes(), stderr.Bytes(), code
	}
	stdout, stderr, code := invoke(nil, "core-info")
	var identity coreprovider.Identity
	if code != 0 || len(stderr) != 0 || decode(stdout, &identity) != nil ||
		!coreprovider.SourceIdentityVerified(identity) || identity.ID != os.Args[2] ||
		identity.ArtifactState != coreprovider.Unknown || identity.ArtifactDigest != "" {
		return errors.New("core-info did not report the expected source-only identity")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return errors.New("occupied-port fixture unavailable")
	}
	defer func() { _ = listener.Close() }()
	logPath, _ := json.Marshal(filepath.Join(root, "must-not-exist.log"))
	valid := []byte(fmt.Sprintf(`{"log":{"output":%s},"inbounds":[{"type":"mixed","tag":"occupied","listen":"127.0.0.1","listen_port":%d}]}`, logPath, listener.Addr().(*net.TCPAddr).Port))
	var passed []string
	for _, test := range []struct {
		name, raw, state, reason string
		digest                   bool
	}{
		{"no-start-no-write", string(valid), "VALID", "", true},
		{"stdout-sink-isolated", `{"log":{"output":"stdout","level":"trace"}}`, "VALID", "", true},
		{"invalid-log-level", `{"log":{"level":"credential-sentinel"}}`, "REJECTED", "TARGET_CONFIGURATION_REJECTED", true},
		{"exact-bytes", " {\n\"log\":{\"disabled\":true}\n} \n", "VALID", "", true},
		{"unknown-root", `{"log":{"disabled":true},"credential-sentinel":true}`, "REJECTED", "TARGET_CONFIGURATION_REJECTED", true},
		{"unknown-option", `{"log":{"disabled":true},"outbounds":[{"type":"direct","credential-sentinel":null}]}`, "REJECTED", "TARGET_CONFIGURATION_REJECTED", true},
		{"duplicate", `{"log":{},"\u006cog":{"disabled":true}}`, "REJECTED", "CONFIGURATION_INPUT_INVALID", false},
		{"trailing-document", `{} {}`, "REJECTED", "CONFIGURATION_INPUT_INVALID", false},
		{"invalid-utf8", "{\"value\":\"\xff\"}", "REJECTED", "CONFIGURATION_INPUT_INVALID", false},
		{"over-limit", strings.Repeat(" ", validation.MaxTargetConfigBytes+1), "REJECTED", "CONFIGURATION_INPUT_INVALID", false},
	} {
		stdout, stderr, code := invoke([]byte(test.raw), "core-check")
		var report validation.TargetCheck
		wantCode := 1
		if test.state == "VALID" {
			wantCode = 0
		}
		if code != wantCode || len(stderr) != 0 || decode(stdout, &report) != nil ||
			report.Schema != validation.TargetCheckSchema || report.State != test.state ||
			report.CoreProvider.Revision != identity.Revision ||
			strings.Contains(string(stdout), "credential-sentinel") {
			return fmt.Errorf("target command failed scenario %s", test.name)
		}
		if test.reason == "" && len(report.ReasonCodes) != 0 || test.reason != "" &&
			(len(report.ReasonCodes) != 1 || report.ReasonCodes[0] != test.reason) {
			return fmt.Errorf("target command gave inconsistent reasons for %s", test.name)
		}
		if test.digest {
			digest := sha256.Sum256([]byte(test.raw))
			if report.ConfigurationDigest != hex.EncodeToString(digest[:]) {
				return fmt.Errorf("target command did not bind exact bytes for %s", test.name)
			}
		} else if report.ConfigurationDigest != "" {
			return fmt.Errorf("invalid input acquired validation evidence for %s", test.name)
		}
		passed = append(passed, test.name)
	}
	for _, command := range []string{"core-info", "core-check"} {
		stdout, stderr, code := invoke(nil, command, "--path", "credential-sentinel")
		if code != 2 || len(stdout) != 0 || len(stderr) == 0 || bytes.Contains(stderr, []byte("credential-sentinel")) {
			return errors.New("maintenance command admitted or echoed a path argument")
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		return errors.New("maintenance command changed its working directory")
	}
	file, err := os.Open(binary)
	if err != nil {
		return errors.New("probe executable digest unavailable")
	}
	digest := sha256.New()
	_, readErr := io.Copy(digest, file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return errors.New("probe executable digest failed")
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Schema       string                `json:"schema"`
		BinaryDigest string                `json:"binaryDigest"`
		Identity     coreprovider.Identity `json:"coreProvider"`
		Scenarios    []string              `json:"passedScenarios"`
	}{"solovey.core-target-probe/v1", hex.EncodeToString(digest.Sum(nil)), identity, passed})
}

func decode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("multiple command reports")
	}
	return nil
}
