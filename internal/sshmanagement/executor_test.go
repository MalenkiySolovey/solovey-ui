package sshmanagement

import (
	"bytes"
	"context"
	"io"
	"testing"
)

type fixedRunnerFixture struct{ output []byte }

func (f fixedRunnerFixture) RunFixed(context.Context, FixedInvocation) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.output)), nil
}

func TestExecutorAllowsOnlyFixedSSHInspectionOperations(t *testing.T) {
	requests := []CommandRequestV1{
		{Kind: CommandVersion, BinaryID: "selected_sshd", ConfigID: "selected_main"},
		{Kind: CommandSyntaxCheck, BinaryID: "selected_sshd", ConfigID: "managed_candidate"},
		{Kind: CommandEffective, BinaryID: "openssh_sshd", ConfigID: "selected_main"},
		{Kind: CommandEffectiveMatch, BinaryID: "selected_sshd", ConfigID: "selected_main", Match: MatchQueryV1{AddressClass: "ipv4_public", UserClass: "administrator", HostClass: "canonical", LocalAddressClass: "ipv4_wildcard", LocalPort: 22}},
	}
	for _, request := range requests {
		if _, err := request.Invocation(); err != nil {
			t.Fatalf("fixed request rejected: %#v: %v", request, err)
		}
	}
	for _, request := range []CommandRequestV1{
		{Kind: CommandKind("SHELL"), BinaryID: "selected_sshd", ConfigID: "selected_main"},
		{Kind: CommandVersion, BinaryID: "/usr/sbin/sshd", ConfigID: "selected_main"},
		{Kind: CommandSyntaxCheck, BinaryID: "selected_sshd", ConfigID: "/etc/ssh/sshd_config"},
		{Kind: CommandEffectiveMatch, BinaryID: "selected_sshd", ConfigID: "selected_main", Match: MatchQueryV1{AddressClass: "1.2.3.4;id", UserClass: "root", HostClass: "canonical", LocalAddressClass: "ipv4_wildcard", LocalPort: 22}},
	} {
		if _, err := request.Invocation(); err == nil {
			t.Fatalf("unsafe invocation accepted: %#v", request)
		}
	}
}

func TestExecutorBoundsOutputAndKeepsItOutOfJSON(t *testing.T) {
	request := CommandRequestV1{Kind: CommandEffective, BinaryID: "selected_sshd", ConfigID: "selected_main", OutputLimit: 4}
	if _, err := ExecuteBounded(context.Background(), fixedRunnerFixture{output: []byte("12345")}, request); err == nil {
		t.Fatal("oversized output accepted")
	}
	result, err := ExecuteBounded(context.Background(), fixedRunnerFixture{output: []byte("ok")}, CommandRequestV1{Kind: CommandEffective, BinaryID: "selected_sshd", ConfigID: "selected_main"})
	if err != nil || result.Output != "ok" || result.OutputDigest == "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
