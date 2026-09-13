package helper

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const managedNFTRollbackArtifactSchemaV1 = "solovey-ui/managed-nft-rollback/v1"

var nftExpiresValue = regexp.MustCompile(`(?:^|[[:space:]])expires[[:space:]]+([0-9]+(?:ns|us|ms|[smhd])(?:[0-9]+(?:ns|us|ms|[smhd]))*)`)

// Dated candidates use the same absolute-expiry mechanism as rollback. The
// dated file remains authenticated by its original SHA; execution material is
// freshly derived and must still match the requested semantic/membership fences.
func materializeManagedNFTCandidate(raw []byte, capturedAt int64, now time.Time) ([]byte, error) {
	if capturedAt == 0 {
		return raw, nil
	}
	captured := time.Unix(0, capturedAt)
	if capturedAt < 0 || captured.After(now) || now.Sub(captured) > time.Minute {
		return nil, errors.New("candidate temporal capture is not current")
	}
	artifact, err := captureManagedNFTRollback(raw, captured)
	if err != nil {
		return nil, err
	}
	data, _, err := materializeManagedNFTRollback(artifact, now)
	return data, err
}

type managedNFTRollbackArtifactV1 struct {
	Schema             string                          `json:"schema"`
	CapturedAtUnixNano int64                           `json:"capturedAtUnixNano"`
	Template           string                          `json:"template"`
	TimedLists         []managedNFTRollbackTimedListV1 `json:"timedLists"`
}

type managedNFTRollbackTimedListV1 struct {
	Token    string                             `json:"token"`
	Elements []managedNFTRollbackTimedElementV1 `json:"elements"`
}

type managedNFTRollbackTimedElementV1 struct {
	Text              string `json:"text"`
	ExpiresAtUnixNano int64  `json:"expiresAtUnixNano,omitempty"`
}

// captureManagedNFTRollback preserves nft's decreasing `expires` value as an
// absolute helper-owned deadline. Untimed before-images retain the legacy raw
// representation so the temporal contract does not perturb unrelated rollback
// artifacts.
func captureManagedNFTRollback(raw []byte, now time.Time) ([]byte, error) {
	if len(raw) == 0 || len(raw) > MaxArtifactBytes {
		return nil, errors.New("managed rollback input size is invalid")
	}
	text := string(raw)
	var template strings.Builder
	lists := make([]managedNFTRollbackTimedListV1, 0)
	for offset := 0; ; {
		relative := strings.Index(text[offset:], "elements = {")
		if relative < 0 {
			template.WriteString(text[offset:])
			break
		}
		start := offset + relative
		bodyStart := start + len("elements = {")
		bodyEndRelative := strings.IndexByte(text[bodyStart:], '}')
		if bodyEndRelative < 0 {
			return nil, errors.New("managed rollback elements are malformed")
		}
		bodyEnd := bodyStart + bodyEndRelative
		body := text[bodyStart:bodyEnd]
		if !nftExpiresValue.MatchString(body) {
			template.WriteString(text[offset : bodyEnd+1])
			offset = bodyEnd + 1
			continue
		}
		elements, err := captureManagedNFTTimedElements(body, now)
		if err != nil {
			return nil, err
		}
		token := fmt.Sprintf("__SOLOVEY_TIMED_ROLLBACK_LIST_%06d__", len(lists))
		template.WriteString(text[offset:bodyStart])
		template.WriteByte(' ')
		template.WriteString(token)
		template.WriteByte(' ')
		lists = append(lists, managedNFTRollbackTimedListV1{Token: token, Elements: elements})
		offset = bodyEnd
	}
	if len(lists) == 0 {
		return append([]byte(nil), raw...), nil
	}
	artifact := managedNFTRollbackArtifactV1{Schema: managedNFTRollbackArtifactSchemaV1,
		CapturedAtUnixNano: now.UTC().UnixNano(), Template: template.String(), TimedLists: lists}
	data, err := json.Marshal(artifact)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if len(data) > MaxArtifactBytes {
		return nil, errors.New("managed rollback artifact exceeds its bound")
	}
	return data, nil
}

func captureManagedNFTTimedElements(body string, now time.Time) ([]managedNFTRollbackTimedElementV1, error) {
	items := strings.Split(body, ",")
	result := make([]managedNFTRollbackTimedElementV1, 0, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		matches := nftExpiresValue.FindAllStringSubmatchIndex(item, -1)
		if len(matches) > 1 {
			return nil, errors.New("managed rollback element has multiple expiry values")
		}
		entry := managedNFTRollbackTimedElementV1{Text: item}
		if len(matches) == 1 {
			match := matches[0]
			duration, err := parseNFTDuration(item[match[2]:match[3]])
			if err != nil {
				return nil, err
			}
			prefix := strings.TrimSpace(item[:match[0]])
			suffix := strings.TrimSpace(item[match[1]:])
			entry.Text = strings.TrimSpace(strings.Join([]string{prefix, suffix}, " "))
			if entry.Text == "" || strings.Contains(entry.Text, "expires") {
				return nil, errors.New("managed rollback element expiry is malformed")
			}
			if duration > time.Duration(math.MaxInt64-now.UTC().UnixNano()) {
				return nil, errors.New("managed rollback element expiry overflows")
			}
			entry.ExpiresAtUnixNano = now.UTC().UnixNano() + int64(duration)
		} else if strings.Contains(" "+item+" ", " timeout ") {
			return nil, errors.New("managed rollback timed element has no remaining expiry")
		}
		if strings.ContainsAny(entry.Text, "{},\r\n") {
			return nil, errors.New("managed rollback element text is malformed")
		}
		result = append(result, entry)
	}
	if len(result) == 0 {
		return nil, errors.New("managed rollback timed element list is empty")
	}
	return result, nil
}

func materializeManagedNFTRollback(artifact []byte, now time.Time) ([]byte, bool, error) {
	if len(artifact) == 0 || len(artifact) > MaxArtifactBytes {
		return nil, false, errors.New("managed rollback artifact size is invalid")
	}
	trimmed := bytes.TrimSpace(artifact)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return append([]byte(nil), artifact...), false, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var value managedNFTRollbackArtifactV1
	if err := decoder.Decode(&value); err != nil {
		return nil, true, errors.New("managed rollback temporal artifact is malformed")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, true, errors.New("managed rollback temporal artifact has trailing data")
	}
	if err := validateManagedNFTRollbackArtifact(value); err != nil {
		return nil, true, err
	}
	materialized := value.Template
	deadline := now.UTC().UnixNano()
	for _, list := range value.TimedLists {
		elements := make([]string, 0, len(list.Elements))
		for _, element := range list.Elements {
			if element.ExpiresAtUnixNano == 0 {
				elements = append(elements, element.Text)
				continue
			}
			remaining := element.ExpiresAtUnixNano - deadline
			if remaining < int64(time.Millisecond) {
				continue
			}
			// Floor to nft's millisecond unit. A restored member can therefore
			// never outlive the captured absolute deadline.
			remainingMilliseconds := remaining / int64(time.Millisecond)
			elements = append(elements, element.Text+" expires "+strconv.FormatInt(remainingMilliseconds, 10)+"ms")
		}
		materialized = strings.Replace(materialized, list.Token, strings.Join(elements, ", "), 1)
	}
	data := []byte(materialized)
	if len(data) == 0 || len(data) > MaxArtifactBytes {
		return nil, true, errors.New("managed rollback materialization size is invalid")
	}
	return data, true, nil
}

func validateManagedNFTRollbackArtifact(value managedNFTRollbackArtifactV1) error {
	if value.Schema != managedNFTRollbackArtifactSchemaV1 || value.CapturedAtUnixNano <= 0 || value.Template == "" || len(value.Template) > MaxArtifactBytes || len(value.TimedLists) == 0 || len(value.TimedLists) > 8192 {
		return errors.New("managed rollback temporal artifact identity is invalid")
	}
	seen := make(map[string]struct{}, len(value.TimedLists))
	for index, list := range value.TimedLists {
		expected := fmt.Sprintf("__SOLOVEY_TIMED_ROLLBACK_LIST_%06d__", index)
		if list.Token != expected || strings.Count(value.Template, list.Token) != 1 || len(list.Elements) == 0 || len(list.Elements) > 8192 {
			return errors.New("managed rollback temporal list identity is invalid")
		}
		if _, exists := seen[list.Token]; exists {
			return errors.New("managed rollback temporal list is duplicated")
		}
		seen[list.Token] = struct{}{}
		for _, element := range list.Elements {
			if strings.TrimSpace(element.Text) == "" || element.Text != strings.TrimSpace(element.Text) || strings.ContainsAny(element.Text, "{},\r\n") || strings.Contains(element.Text, "expires") || element.ExpiresAtUnixNano < 0 || element.ExpiresAtUnixNano > 0 && element.ExpiresAtUnixNano < value.CapturedAtUnixNano {
				return errors.New("managed rollback temporal element is invalid")
			}
		}
	}
	return nil
}

func parseNFTDuration(value string) (time.Duration, error) {
	units := map[string]int64{"ns": 1, "us": int64(time.Microsecond), "ms": int64(time.Millisecond), "s": int64(time.Second), "m": int64(time.Minute), "h": int64(time.Hour), "d": int64(24 * time.Hour)}
	var total int64
	for offset := 0; offset < len(value); {
		start := offset
		for offset < len(value) && value[offset] >= '0' && value[offset] <= '9' {
			offset++
		}
		if offset == start {
			return 0, errors.New("managed rollback nft duration is malformed")
		}
		number, err := strconv.ParseInt(value[start:offset], 10, 64)
		if err != nil {
			return 0, errors.New("managed rollback nft duration is malformed")
		}
		unit := ""
		for _, candidate := range []string{"ms", "us", "ns", "s", "m", "h", "d"} {
			if strings.HasPrefix(value[offset:], candidate) {
				unit = candidate
				break
			}
		}
		multiplier := units[unit]
		if unit == "" || number <= 0 || number > math.MaxInt64/multiplier || total > math.MaxInt64-number*multiplier {
			return 0, errors.New("managed rollback nft duration is invalid")
		}
		total += number * multiplier
		offset += len(unit)
	}
	return time.Duration(total), nil
}
