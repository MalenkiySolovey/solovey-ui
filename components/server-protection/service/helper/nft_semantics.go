package helper

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ManagedNFTSemanticSchemaV1 identifies the single semantic identity used for
// current-state fences. It is deliberately independent of the candidate or
// rollback artifact byte identity.
const (
	ManagedNFTSemanticSchemaV1        = "solovey-ui/managed-nft-semantic/v1"
	ManagedNFTTimedMembershipSchemaV1 = "solovey-ui/managed-nft-timed-membership/v1"
)

var (
	nftHandleSuffix   = regexp.MustCompile(`\s+#\s*(?:handle|count)\s+[0-9]+\s*$`)
	nftCounterStats   = regexp.MustCompile(`\bcounter(?:\s+packets\s+[0-9]+\s+bytes\s+[0-9]+)?\b`)
	nftTimeoutValue   = regexp.MustCompile(`\s+(?:timeout|expires)\s+[0-9a-z]+`)
	nftRevisionLine   = regexp.MustCompile(`^comment\s+"solovey-revision:([a-f0-9]{16,128})"$`)
	nftAbsoluteTime   = regexp.MustCompile(`\bmeta time < (?:"([0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2})"|([0-9]{1,12}))`)
	nftIPv4HostPrefix = regexp.MustCompile(`\b([0-9]{1,3}(?:\.[0-9]{1,3}){3})/32\b`)
	nftIPv6HostPrefix = regexp.MustCompile(`\b([0-9a-f:]+)/128\b`)
)

// ManagedNFTObservation is internal to the restricted helper. Raw output is
// retained only to build rollback material and never crosses the protocol.
type managedNFTObservation struct {
	present            bool
	revision           string
	semanticSHA        string
	timedMembershipSHA string
	raw                []byte
}

func managedSemanticSHA(data []byte) (string, error) {
	canonical, _, err := canonicalManagedNFT(data, false)
	if err != nil {
		return "", err
	}
	return sha256Hex(append([]byte(ManagedNFTSemanticSchemaV1+"\x00"), canonical...)), nil
}

// ManagedSemanticSHA256 is exported for the unprivileged workflow so it can
// persist the candidate semantic identity. It does not expose live nft text.
func ManagedSemanticSHA256(data []byte) (string, error) { return managedSemanticSHA(data) }

func managedTimedMembershipSHA(data []byte) (string, error) {
	_, _, membership, err := canonicalManagedNFTProjection(data, false)
	if err != nil {
		return "", err
	}
	return sha256Hex(append([]byte(ManagedNFTTimedMembershipSchemaV1+"\x00"), membership...)), nil
}

// ManagedTimedMembershipSHA256 identifies the exact members of all owned
// timeout sets while deliberately excluding their decreasing remaining TTL.
// Static set/rule topology remains covered by ManagedSemanticSHA256.
func ManagedTimedMembershipSHA256(data []byte) (string, error) {
	return managedTimedMembershipSHA(data)
}

func observeManagedTable(ctx context.Context, executor NFTExecutor) (managedNFTObservation, error) {
	raw, present, err := executor.ListManagedTable(ctx)
	if err != nil {
		return managedNFTObservation{}, err
	}
	if !present {
		return managedNFTObservation{}, nil
	}
	canonical, revision, membership, err := canonicalManagedNFTProjection(raw, false)
	if err != nil {
		return managedNFTObservation{}, fmt.Errorf("managed table semantic observation failed: %w", err)
	}
	return managedNFTObservation{present: true, revision: revision,
		semanticSHA:        sha256Hex(append([]byte(ManagedNFTSemanticSchemaV1+"\x00"), canonical...)),
		timedMembershipSHA: sha256Hex(append([]byte(ManagedNFTTimedMembershipSchemaV1+"\x00"), membership...)),
		raw:                append([]byte(nil), raw...)}, nil
}

// canonicalManagedNFT accepts the bounded generated grammar and the stable
// `nft list table` representation. Handles, counter values, formatting and
// aging timeout values are volatile; chain rule order and all other statements
// remain semantic. Set/chain declaration order is irrelevant and sorted.
func canonicalManagedNFT(data []byte, allowDelete bool) ([]byte, string, error) {
	canonical, revision, _, err := canonicalManagedNFTProjection(data, allowDelete)
	return canonical, revision, err
}

func canonicalManagedNFTProjection(data []byte, allowDelete bool) ([]byte, string, []byte, error) {
	if len(data) == 0 || len(data) > MaxArtifactBytes {
		return nil, "", nil, errors.New("managed table semantic input size is invalid")
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	canonicalLines := make([]string, 0, len(lines))
	depth := 0
	seenTable := false
	var revision string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		line = nftHandleSuffix.ReplaceAllString(line, "")
		line = nftCounterStats.ReplaceAllString(line, "counter")
		line = normalizeNFTEndpointExpressions(line)
		normalizedLine, err := normalizeNFTAbsoluteTime(line)
		if err != nil {
			return nil, "", nil, err
		}
		line = normalizedLine
		line = normalizeNFTElements(line)
		line = strings.Join(strings.Fields(line), " ")
		line = strings.ReplaceAll(line, "priority filter - 5", "priority -5")
		line = normalizeNFTDuration(line)
		if line == "delete table inet solovey_protection" {
			if !allowDelete {
				return nil, "", nil, errors.New("managed semantic input is a deletion")
			}
			return []byte(line + "\n"), "", nil, nil
		}
		if depth == 0 && line == "table inet solovey_protection {" {
			seenTable = true
		}
		if depth == 0 && strings.HasPrefix(line, "table ") && line != "table inet solovey_protection {" {
			return nil, "", nil, errors.New("managed semantic input names a foreign table")
		}
		if line == "}" && depth == 0 {
			return nil, "", nil, errors.New("managed semantic input has unbalanced scope")
		}
		if match := nftRevisionLine.FindStringSubmatch(line); match != nil {
			if revision != "" {
				return nil, "", nil, errors.New("managed semantic input has multiple revisions")
			}
			revision = match[1]
		}
		canonicalLines = append(canonicalLines, line)
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth < 0 {
			return nil, "", nil, errors.New("managed semantic input has unbalanced scope")
		}
	}
	if !seenTable || depth != 0 || revision == "" {
		return nil, "", nil, errors.New("managed semantic input has no complete owned table")
	}
	canonicalLines, timedMembership, err := sortManagedChildren(canonicalLines)
	if err != nil {
		return nil, "", nil, err
	}
	return []byte(strings.Join(canonicalLines, "\n") + "\n"), revision, []byte(strings.Join(timedMembership, "\n") + "\n"), nil
}

func normalizeNFTEndpointExpressions(line string) string {
	line = strings.ReplaceAll(line, "meta nfproto ipv4 ip ", "ip ")
	line = strings.ReplaceAll(line, "meta nfproto ipv6 ip6 ", "ip6 ")
	line = strings.ReplaceAll(line, "meta l4proto tcp tcp ", "tcp ")
	line = strings.ReplaceAll(line, "meta l4proto udp udp ", "udp ")
	line = nftIPv4HostPrefix.ReplaceAllString(line, "$1")
	return nftIPv6HostPrefix.ReplaceAllString(line, "$1")
}

func normalizeNFTAbsoluteTime(line string) (string, error) {
	matches := nftAbsoluteTime.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		return line, nil
	}
	if len(matches) != 1 {
		return "", errors.New("managed semantic input has multiple absolute time expressions")
	}
	match := matches[0]
	value := ""
	if match[2] >= 0 {
		parsed, err := time.ParseInLocation("2006-01-02 15:04:05", line[match[2]:match[3]], time.UTC)
		if err != nil {
			return "", errors.New("managed semantic input has malformed absolute time")
		}
		value = strconv.FormatInt(parsed.Unix(), 10)
	} else {
		seconds, err := strconv.ParseInt(line[match[4]:match[5]], 10, 64)
		if err != nil || seconds <= 0 {
			return "", errors.New("managed semantic input has malformed absolute time")
		}
		value = strconv.FormatInt(seconds, 10)
	}
	return line[:match[0]] + "meta time < " + value + line[match[1]:], nil
}

func normalizeNFTDuration(line string) string {
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "timeout" {
		return line
	}
	value := fields[1]
	multiplier := int64(1)
	for suffix, seconds := range map[string]int64{"s": 1, "m": 60, "h": 3600, "d": 86400} {
		if strings.HasSuffix(value, suffix) {
			multiplier = seconds
			value = strings.TrimSuffix(value, suffix)
			break
		}
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number <= 0 {
		return line
	}
	return "timeout " + strconv.FormatInt(number*multiplier, 10) + "s"
}

func normalizeNFTElements(line string) string {
	start := strings.Index(line, "elements = {")
	if start < 0 || !strings.HasSuffix(strings.TrimSpace(line), "}") {
		return line
	}
	prefix := line[:start+len("elements = {")]
	body := strings.TrimSpace(strings.TrimSuffix(line[start+len("elements = {"):], "}"))
	if body == "" {
		return prefix + " }"
	}
	items := strings.Split(body, ",")
	for i := range items {
		items[i] = strings.TrimSpace(nftTimeoutValue.ReplaceAllString(items[i], ""))
	}
	sort.Strings(items)
	return prefix + " " + strings.Join(items, ", ") + " }"
}

func sortManagedChildren(lines []string) ([]string, []string, error) {
	if len(lines) < 3 {
		return nil, nil, errors.New("managed semantic input has no owned objects")
	}
	children := make([][]string, 0, len(lines))
	timedMembership := make([]string, 0)
	seen := make(map[string]struct{})
	comment := ""
	for index := 1; index < len(lines)-1; {
		line := lines[index]
		if nftRevisionLine.MatchString(line) {
			if comment != "" {
				return nil, nil, errors.New("managed semantic input has multiple owner comments")
			}
			comment = line
			index++
			continue
		}
		kind := ""
		switch {
		case strings.HasPrefix(line, "set ") && strings.HasSuffix(line, " {"):
			kind = "set"
		case strings.HasPrefix(line, "chain ") && strings.HasSuffix(line, " {"):
			kind = "chain"
		default:
			return nil, nil, errors.New("managed semantic input has an unexpected table-level statement")
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, nil, errors.New("managed semantic input has a malformed object declaration")
		}
		identity := kind + ":" + fields[1]
		if _, duplicate := seen[identity]; duplicate {
			return nil, nil, errors.New("managed semantic input has a duplicate managed object")
		}
		seen[identity] = struct{}{}
		block := []string{line}
		depth := strings.Count(line, "{") - strings.Count(line, "}")
		index++
		for index < len(lines)-1 && depth > 0 {
			block = append(block, lines[index])
			depth += strings.Count(lines[index], "{") - strings.Count(lines[index], "}")
			index++
		}
		if depth != 0 || block[len(block)-1] != "}" {
			return nil, nil, errors.New("managed semantic input has a malformed object scope")
		}
		if kind == "set" {
			var err error
			var members []string
			block, members, err = canonicalizeSetBlock(block)
			if err != nil {
				return nil, nil, err
			}
			for _, member := range members {
				timedMembership = append(timedMembership, fields[1]+"\x00"+member)
			}
		}
		children = append(children, block)
	}
	if comment == "" {
		return nil, nil, errors.New("managed semantic input has no owner comment")
	}
	sort.SliceStable(children, func(i, j int) bool { return strings.Join(children[i], "\x00") < strings.Join(children[j], "\x00") })
	result := []string{lines[0], comment}
	for _, child := range children {
		result = append(result, child...)
	}
	result = append(result, "}")
	sort.Strings(timedMembership)
	return result, timedMembership, nil
}

func canonicalizeSetBlock(block []string) ([]string, []string, error) {
	if len(block) < 3 {
		return nil, nil, errors.New("managed semantic input has an empty set")
	}
	timed := false
	for _, line := range block[1 : len(block)-1] {
		if strings.HasPrefix(line, "flags ") {
			flags := strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "flags ")), ",")
			for _, flag := range flags {
				if strings.TrimSpace(flag) == "timeout" {
					timed = true
				}
			}
		}
	}
	body := make([]string, 0, len(block))
	members := make([]string, 0)
	for index := 1; index < len(block)-1; index++ {
		line := block[index]
		if strings.HasPrefix(line, "flags ") {
			flags := strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "flags ")), ",")
			for i := range flags {
				flags[i] = strings.TrimSpace(flags[i])
			}
			sort.Strings(flags)
			body = append(body, "flags "+strings.Join(flags, ","))
			continue
		}
		if strings.HasPrefix(line, "elements = {") {
			combined := line
			depth := strings.Count(line, "{") - strings.Count(line, "}")
			for depth > 0 {
				index++
				if index >= len(block)-1 {
					return nil, nil, errors.New("managed semantic input has malformed set elements")
				}
				combined += " " + block[index]
				depth += strings.Count(block[index], "{") - strings.Count(block[index], "}")
			}
			normalized := normalizeNFTElements(strings.Join(strings.Fields(combined), " "))
			if timed {
				members = append(members, nftElementMembers(normalized)...)
			} else {
				body = append(body, normalized)
			}
			continue
		}
		body = append(body, line)
	}
	if timed {
		// Membership and per-element remaining lifetime are runtime state for
		// timeout sets. The set type/flags/size/default timeout remain static.
		body = append(body, "elements = { <runtime-managed> }")
	}
	sort.Strings(body)
	result := append([]string{block[0]}, body...)
	return append(result, "}"), members, nil
}

func nftElementMembers(line string) []string {
	start := strings.Index(line, "elements = {")
	if start < 0 || !strings.HasSuffix(strings.TrimSpace(line), "}") {
		return nil
	}
	body := strings.TrimSpace(strings.TrimSuffix(line[start+len("elements = {"):], "}"))
	if body == "" {
		return nil
	}
	items := strings.Split(body, ",")
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}
