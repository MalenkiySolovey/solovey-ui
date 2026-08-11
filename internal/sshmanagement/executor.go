package sshmanagement

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	MaxExecutorOutputBytes = 256 * 1024
	MaxExecutorDuration    = 5 * time.Second
)

type CommandKind string

const (
	CommandVersion        CommandKind = "VERSION"
	CommandSyntaxCheck    CommandKind = "SYNTAX_CHECK"
	CommandEffective      CommandKind = "EFFECTIVE_GLOBAL"
	CommandEffectiveMatch CommandKind = "EFFECTIVE_MATCH"
)

type MatchQueryV1 struct {
	AddressClass      string `json:"addressClass"`
	UserClass         string `json:"userClass"`
	HostClass         string `json:"hostClass"`
	LocalAddressClass string `json:"localAddressClass"`
	LocalPort         uint16 `json:"localPort"`
	RoutingDomain     string `json:"routingDomain,omitempty"`
}

type CommandRequestV1 struct {
	Kind        CommandKind   `json:"kind"`
	BinaryID    string        `json:"binaryId"`
	ConfigID    string        `json:"configId"`
	Match       MatchQueryV1  `json:"match,omitempty"`
	Timeout     time.Duration `json:"-"`
	OutputLimit int64         `json:"-"`
}

type FixedInvocation struct {
	BinaryID string
	Args     []string
	Timeout  time.Duration
	MaxBytes int64
}

func (r CommandRequestV1) Invocation() (FixedInvocation, error) {
	if !oneOf(r.BinaryID, "selected_sshd", "openssh_sshd") || !oneOf(r.ConfigID, "selected_main", "managed_candidate") {
		return FixedInvocation{}, NewError("executor", ReasonMalformedProviderEvidence)
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = MaxExecutorDuration
	}
	limit := r.OutputLimit
	if limit <= 0 {
		limit = MaxExecutorOutputBytes
	}
	if timeout > MaxExecutorDuration || limit > MaxExecutorOutputBytes {
		return FixedInvocation{}, NewError("executor", ReasonMalformedProviderEvidence)
	}
	invocation := FixedInvocation{BinaryID: r.BinaryID, Timeout: timeout, MaxBytes: limit}
	switch r.Kind {
	case CommandVersion:
		invocation.Args = []string{"-V"}
	case CommandSyntaxCheck:
		invocation.Args = []string{"-t", "-f", r.ConfigID}
	case CommandEffective:
		invocation.Args = []string{"-T", "-f", r.ConfigID}
	case CommandEffectiveMatch:
		if !oneOf(r.Match.AddressClass, "ipv4_public", "ipv4_private", "ipv6_public", "ipv6_private", "loopback") ||
			!oneOf(r.Match.UserClass, "root", "administrator", "non_privileged") ||
			!oneOf(r.Match.HostClass, "canonical", "uncanonicalized", "local") ||
			!oneOf(r.Match.LocalAddressClass, "ipv4_wildcard", "ipv6_wildcard", "loopback") || r.Match.LocalPort == 0 ||
			r.Match.RoutingDomain != "" && !oneOf(r.Match.RoutingDomain, "default") {
			return FixedInvocation{}, NewError("executor", ReasonUnknownMatchContext)
		}
		criteria := "addr=" + r.Match.AddressClass + ",user=" + r.Match.UserClass + ",host=" + r.Match.HostClass +
			",laddr=" + r.Match.LocalAddressClass + fmt.Sprintf(",lport=%d", r.Match.LocalPort)
		if r.Match.RoutingDomain != "" {
			criteria += ",rdomain=" + r.Match.RoutingDomain
		}
		invocation.Args = []string{"-T", "-f", r.ConfigID, "-C", criteria}
	default:
		return FixedInvocation{}, NewError("executor", ReasonUnsupportedDirective)
	}
	return invocation, nil
}

type FixedRunner interface {
	RunFixed(context.Context, FixedInvocation) (io.ReadCloser, error)
}

type CommandResultV1 struct {
	Kind         CommandKind `json:"kind"`
	Output       string      `json:"-"`
	OutputDigest string      `json:"outputDigest"`
	Bytes        int         `json:"bytes"`
}

// ExecuteBounded accepts only an already validated fixed invocation and never
// exposes command output in its public JSON shape.
func ExecuteBounded(ctx context.Context, runner FixedRunner, request CommandRequestV1) (CommandResultV1, error) {
	if runner == nil {
		return CommandResultV1{}, NewError("executor", ReasonProviderUnavailable)
	}
	invocation, err := request.Invocation()
	if err != nil {
		return CommandResultV1{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, invocation.Timeout)
	defer cancel()
	reader, err := runner.RunFixed(bounded, invocation)
	if err != nil {
		return CommandResultV1{}, NewError("executor", ReasonProviderUnavailable)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, invocation.MaxBytes+1))
	if err != nil || int64(len(data)) > invocation.MaxBytes {
		return CommandResultV1{}, NewError("executor", ReasonMalformedProviderEvidence)
	}
	if strings.IndexByte(string(data), 0) >= 0 {
		return CommandResultV1{}, NewError("executor", ReasonMalformedProviderEvidence)
	}
	result := CommandResultV1{Kind: request.Kind, Output: string(data), OutputDigest: Revision(string(data)), Bytes: len(data)}
	if errors.Is(bounded.Err(), context.DeadlineExceeded) {
		return CommandResultV1{}, NewError("executor", ReasonProviderUnavailable)
	}
	return result, nil
}
