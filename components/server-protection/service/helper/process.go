package helper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	ExitOK                = 0
	ExitInvalidRequest    = 64
	ExitMissingCapability = 69
	ExitTimeout           = 75
	ExitInternal          = 70
)

type ProcessResult struct {
	Stdout          []byte
	Stderr          []byte
	StdoutTruncated bool
	StderrTruncated bool
	ExitCode        int
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = b.truncated || original > 0
		return original, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(value)
	return original, nil
}

type ProcessInvoker struct {
	binary TrustedBinary
	root   ManagedRoot
	env    []string
}

func NewProcessInvoker(binary TrustedBinary, root ManagedRoot) *ProcessInvoker {
	return &ProcessInvoker{
		binary: binary,
		root:   root,
		env:    []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"},
	}
}

// DiscoverInstalledProcessInvoker accepts only the release-layout sibling
// helper. There is intentionally no environment variable, settings value, or
// HTTP field that can choose a binary path.
func DiscoverInstalledProcessInvoker(root ManagedRoot) (Invoker, string) {
	if runtime.GOOS != "linux" {
		return nil, "linux_required"
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, "panel_executable_unknown"
	}
	installRoot := filepath.Dir(executable)
	binary, err := NewTrustedBinary(installRoot, filepath.Join(installRoot, "solovey-protect-helper"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "helper_not_installed"
		}
		return nil, "helper_identity_mismatch"
	}
	return NewProcessInvoker(binary, root), ""
}

func (p *ProcessInvoker) Invoke(ctx context.Context, request Request) (Response, InvocationFacts, error) {
	if p == nil || p.binary.path == "" || p.root.path == "" {
		return Response{}, InvocationFacts{}, errors.New("process helper is not configured")
	}
	if err := p.binary.Verify(); err != nil {
		return Response{}, InvocationFacts{ExitClass: "not_started"}, ErrHelperIdentityMismatch
	}
	data, err := EncodeRequest(request)
	if err != nil {
		return Response{}, InvocationFacts{}, err
	}
	stdout := &boundedBuffer{limit: MaxOutputBytes}
	stderr := &boundedBuffer{limit: MaxOutputBytes}
	// The executable and its zero-length argument list are fixed by trusted
	// installation policy. No shell, user command, flags or environment enter
	// this boundary.
	command := exec.CommandContext(ctx, p.binary.path)
	command.Dir = p.root.path
	command.Env = append([]string(nil), p.env...)
	command.Stdin = bytes.NewReader(data)
	command.Stdout = stdout
	command.Stderr = stderr
	err = command.Run()
	result := ProcessResult{
		Stdout: stdout.buffer.Bytes(), Stderr: stderr.buffer.Bytes(),
		StdoutTruncated: stdout.truncated, StderrTruncated: stderr.truncated,
		ExitCode: 0,
	}
	if ctx.Err() != nil {
		return Response{}, invocationFacts(result), ctx.Err()
	}
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			result.ExitCode = exitError.ExitCode()
			response, decodeErr := DecodeResponse(result.Stdout)
			if decodeErr == nil {
				return response, invocationFacts(result), nil
			}
			return Response{}, invocationFacts(result), fmt.Errorf("helper exited as %s", MapProcessExit(result.ExitCode))
		}
		return Response{}, invocationFacts(result), fmt.Errorf("start helper: %w", err)
	}
	response, err := DecodeResponse(result.Stdout)
	return response, invocationFacts(result), err
}

func (p *ProcessInvoker) HelperIdentityRevision() string {
	if p == nil {
		return ""
	}
	return p.binary.IdentityRevision()
}

func invocationFacts(result ProcessResult) InvocationFacts {
	return InvocationFacts{
		ExitClass:       MapProcessExit(result.ExitCode),
		StdoutTruncated: result.StdoutTruncated,
		StderrTruncated: result.StderrTruncated,
	}
}

func MapProcessExit(exitCode int) string {
	switch exitCode {
	case ExitOK:
		return "ok"
	case ExitInvalidRequest:
		return "invalid_request"
	case ExitMissingCapability:
		return "missing_capability"
	case ExitTimeout:
		return "timeout"
	case ExitInternal:
		return "internal_error"
	default:
		return "process_failed"
	}
}

func ExitCodeForResponse(response Response) int {
	if response.OK {
		return ExitOK
	}
	switch response.Code {
	case CodeInvalidRequest, CodePathForbidden, CodeValidationFailed:
		return ExitInvalidRequest
	case CodeMissingCapability, CodeUnsupported:
		return ExitMissingCapability
	case CodeTimeout, CodeCanceled:
		return ExitTimeout
	default:
		return ExitInternal
	}
}
