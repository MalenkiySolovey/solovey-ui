package helper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const managedTable = "inet solovey_protection"

var managedRevisionMarker = regexp.MustCompile(`(?m)^\s*comment "solovey-revision:([a-f0-9]{16,128})"\s*$`)

const nftTTLCapabilityProbe = "table inet solovey_capability_probe {\n  set ttl4 {\n    type ipv4_addr\n    flags timeout\n    timeout 60s\n    elements = { 192.0.2.1 timeout 30s }\n  }\n}\n"
const nftRateCapabilityProbe = "table inet solovey_capability_probe {\n  chain input {\n    type filter hook input priority -5; policy accept;\n    ip saddr 192.0.2.1 limit rate over 20/second burst 40 packets counter drop\n  }\n}\n"

// NFTSupport is explicit so an unknown platform, binary, or primitive never
// becomes an affirmative capability by inference.
type NFTSupport struct {
	PlatformKnown   bool   `json:"platform_known"`
	Linux           bool   `json:"linux"`
	Available       bool   `json:"available"`
	Version         string `json:"version,omitempty"`
	TTLSet          bool   `json:"ttl_set"`
	RateLimit       bool   `json:"rate_limit"`
	TTLSetReason    string `json:"ttl_set_reason,omitempty"`
	RateLimitReason string `json:"rate_limit_reason,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// NFTExecutor is the complete privileged command vocabulary. It has no method
// accepting a binary, argument list, command string, table, family, or stdin.
type NFTExecutor interface {
	Detect(context.Context) NFTSupport
	CheckManagedFile(context.Context, string) error
	ListManagedTable(context.Context) ([]byte, bool, error)
	ApplyManagedFile(context.Context, string) error
}

type systemNFTExecutor struct{ binary string }

func newSystemNFTExecutor() NFTExecutor {
	if runtime.GOOS != "linux" {
		return systemNFTExecutor{}
	}
	for _, candidate := range []string{"/usr/sbin/nft", "/usr/bin/nft", "/sbin/nft", "/bin/nft"} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return systemNFTExecutor{binary: candidate}
		}
	}
	return systemNFTExecutor{}
}

func (e systemNFTExecutor) Detect(ctx context.Context) NFTSupport {
	if runtime.GOOS != "linux" {
		return NFTSupport{PlatformKnown: true, Linux: false, Reason: "linux_required"}
	}
	if e.binary == "" {
		return NFTSupport{PlatformKnown: true, Linux: true, Reason: "nft_not_installed"}
	}
	out, _, err := e.run(ctx, "--version")
	if err != nil {
		return NFTSupport{PlatformKnown: true, Linux: true, Reason: "nft_version_unknown"}
	}
	version := strings.TrimSpace(string(out))
	if len(version) > 128 {
		version = version[:128]
	}
	if _, _, err := e.run(ctx, "list", "tables"); err != nil {
		return NFTSupport{PlatformKnown: true, Linux: true, Version: version, Reason: "nft_access_unavailable"}
	}
	_, _, ttlErr := e.runWithInput(ctx, []byte(nftTTLCapabilityProbe), nftCapabilityCheckArguments()...)
	_, _, rateErr := e.runWithInput(ctx, []byte(nftRateCapabilityProbe), nftCapabilityCheckArguments()...)
	return nftSupportFromPrimitiveChecks(version, ttlErr, rateErr)
}

func nftCapabilityCheckArguments() []string {
	return []string{"--check", "--file", "-"}
}

func nftSupportFromPrimitiveChecks(version string, ttlErr, rateErr error) NFTSupport {
	support := NFTSupport{PlatformKnown: true, Linux: true, Available: true, Version: version, TTLSet: ttlErr == nil, RateLimit: rateErr == nil}
	if ttlErr != nil {
		support.TTLSetReason = "ttl_set_check_failed"
	}
	if rateErr != nil {
		support.RateLimitReason = "rate_limit_check_failed"
	}
	if !support.TTLSet || !support.RateLimit {
		support.Reason = "advanced_primitives_partially_unproven"
	}
	return support
}

func (e systemNFTExecutor) CheckManagedFile(ctx context.Context, path string) error {
	_, _, err := e.run(ctx, "-c", "-f", path)
	return err
}

func (e systemNFTExecutor) ListManagedTable(ctx context.Context) ([]byte, bool, error) {
	out, stderr, err := e.run(ctx, "list", "table", "inet", "solovey_protection")
	if err != nil {
		message := strings.ToLower(string(stderr))
		if strings.Contains(message, "no such file") || strings.Contains(message, "does not exist") {
			return nil, false, nil
		}
		return nil, false, err
	}
	return out, true, nil
}

func (e systemNFTExecutor) ApplyManagedFile(ctx context.Context, path string) error {
	_, _, err := e.run(ctx, "-f", path)
	return err
}

func (e systemNFTExecutor) run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	return e.runWithInput(ctx, nil, args...)
}

func (e systemNFTExecutor) runWithInput(ctx context.Context, input []byte, args ...string) ([]byte, []byte, error) {
	if e.binary == "" {
		return nil, nil, errors.New("nft capability is unavailable")
	}
	stdout, stderr := &boundedBuffer{limit: MaxOutputBytes}, &boundedBuffer{limit: MaxOutputBytes}
	command := exec.CommandContext(ctx, e.binary, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()
	if stdout.truncated || stderr.truncated {
		return stdout.buffer.Bytes(), stderr.buffer.Bytes(), errors.New("nft output exceeded the bounded limit")
	}
	if err != nil {
		return stdout.buffer.Bytes(), stderr.buffer.Bytes(), fmt.Errorf("restricted nft operation failed: %w", err)
	}
	return stdout.buffer.Bytes(), stderr.buffer.Bytes(), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validateCandidate(data []byte, revision, expectedSHA string) error {
	if len(data) == 0 || len(data) > MaxArtifactBytes {
		return errors.New("candidate artifact size is invalid")
	}
	if sha256Hex(data) != expectedSHA {
		return errors.New("candidate artifact SHA-256 mismatch")
	}
	if !bytes.Contains(data, []byte(`comment "solovey-revision:`+revision+`"`)) {
		return errors.New("candidate artifact revision marker mismatch")
	}
	if err := validateManagedScope(data, false); err != nil {
		return err
	}
	allowedSet := regexp.MustCompile(`^set (solovey_allow_tcp_ports|solovey_allow_udp_ports|solovey_graylist4|solovey_graylist6|solovey_(gray|rate|quarantine|block)[46]_[a-f0-9]{12}) \{$`)
	allowedChain := regexp.MustCompile(`^chain (solovey_input_precheck|solovey_tcp_public|solovey_udp_public|solovey_input|solovey_endpoint_[a-f0-9]{12}) \{$`)
	allowedElements := regexp.MustCompile(`^elements = \{ [0-9a-f:.,/ ]* \}$`)
	allowedTimedElements := regexp.MustCompile(`^elements = \{ [0-9a-f:./]+ timeout [0-9]{1,5}s(, [0-9a-f:./]+ timeout [0-9]{1,5}s)* \}$`)
	allowedSize := regexp.MustCompile(`^(size [1-9][0-9]{0,4}|timeout [1-9][0-9]{0,4}s)$`)
	allowedEndpointJump := regexp.MustCompile(`^meta nfproto ipv([46])( ip6? saddr [0-9a-f:./]+)?( ip6? daddr [0-9a-f:.]+)? meta l4proto (tcp|udp) (tcp|udp) dport [0-9]{1,5} (counter accept|jump solovey_endpoint_[a-f0-9]{12})$`)
	allowedEndpointAction := regexp.MustCompile(`^(ip|ip6) saddr @solovey_(gray|rate|quarantine|block)[46]_[a-f0-9]{12}( limit rate over (2|5|20)/second burst (4|10|40) packets)? counter drop$`)
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if match := allowedEndpointJump.FindStringSubmatch(line); match != nil {
			family := match[1]
			source := match[2]
			destination := match[3]
			metaProtocol := match[4]
			portProtocol := match[5]
			if metaProtocol != portProtocol || family == "4" && (strings.HasPrefix(source, " ip6 ") || strings.HasPrefix(destination, " ip6 ")) || family == "6" && (strings.HasPrefix(source, " ip ") || strings.HasPrefix(destination, " ip ")) {
				return fmt.Errorf("candidate contains a conflated endpoint family or protocol")
			}
			continue
		}
		switch {
		case line == "", line == "}", line == "table inet solovey_protection {":
		case line == `comment "solovey-revision:`+revision+`"`:
		case allowedSet.MatchString(line), allowedChain.MatchString(line), allowedElements.MatchString(line), allowedTimedElements.MatchString(line):
		case line == "type inet_service", line == "type ipv4_addr", line == "type ipv6_addr", line == "flags interval", line == "flags interval,timeout":
		case allowedSize.MatchString(line):
		case line == "type filter hook input priority filter; policy accept;":
		case line == "type filter hook input priority -5; policy accept;":
		case line == `iifname "lo" counter accept`, line == "ct state established,related counter accept":
		case line == "meta l4proto tcp tcp dport @solovey_allow_tcp_ports counter accept":
		case line == "meta l4proto udp udp dport @solovey_allow_udp_ports counter accept":
		case allowedEndpointAction.MatchString(line):
		case line == "counter accept":
		default:
			return fmt.Errorf("candidate contains a non-generated nft statement")
		}
	}
	return nil
}

func validateManagedScope(data []byte, allowDelete bool) error {
	text := strings.ToLower(string(data))
	for _, forbidden := range []string{"flush ruleset", "include ", "define ", "iptables", "firewalld", "table ip ", "table ip6 ", "table arp ", "table bridge ", "table netdev "} {
		if strings.Contains(text, forbidden) {
			return fmt.Errorf("managed artifact contains forbidden token %q", forbidden)
		}
	}
	lines := strings.Split(text, "\n")
	found := false
	depth := 0
	tableDeclarations := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if depth == 0 && strings.HasPrefix(line, "table ") {
			if line != "table inet solovey_protection {" {
				return errors.New("artifact names an unmanaged table")
			}
			found = true
			tableDeclarations++
		}
		if depth == 0 && strings.HasPrefix(line, "delete table ") {
			if !allowDelete || line != "delete table inet solovey_protection" {
				return errors.New("artifact contains a forbidden table deletion")
			}
			found = true
			continue
		}
		if depth == 0 && line != "table inet solovey_protection {" {
			return errors.New("artifact contains a top-level command outside the managed table")
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth < 0 {
			return errors.New("artifact has unbalanced managed-table scope")
		}
	}
	if !found || depth != 0 || tableDeclarations > 1 {
		return errors.New("managed table declaration is missing")
	}
	return nil
}

func managedRevision(data []byte) (string, error) {
	if err := validateManagedScope(data, false); err != nil {
		return "", err
	}
	matches := managedRevisionMarker.FindAllSubmatch(data, -1)
	if len(matches) != 1 || !validRevision(string(matches[0][1])) {
		return "", errors.New("managed table has no unique valid revision marker")
	}
	return string(matches[0][1]), nil
}

func verifyManagedRevision(data []byte, expected string) error {
	revision, err := managedRevision(data)
	if err != nil {
		return err
	}
	if revision != expected {
		return errors.New("managed table revision verification failed")
	}
	return nil
}

func writeManagedAtomic(root ManagedRoot, relative string, data []byte) (string, error) {
	path, err := root.Resolve(relative, false)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".nft-*.tmp")
	if err != nil {
		return "", err
	}
	tempPath := temporary.Name()
	defer os.Remove(tempPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return "", err
	}
	return path, os.Chmod(path, 0o600)
}
