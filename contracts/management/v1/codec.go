// Package managementv1 is the transport-neutral public management contract.
// It owns encoding/shape checks only, never authentication, storage or execution.
package managementv1

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const Version = "solovey.management/v1"
const MaxDocumentBytes = 256 * 1024
const maxInteger = int64(9007199254740991)

// Schema is checked in with generated types. Consumers pin the whole manifest.
//
//go:embed schema.json
var schemaJSON []byte

type schemaSet struct {
	Definitions map[string]map[string]any `json:"$defs"`
	Actions     []ActionDescriptor        `json:"x-actions"`
	Inputs      map[string]string         `json:"x-inputs"`
	Reasons     []string                  `json:"x-reasons"`
}

var contract = loadSchema()

func loadSchema() schemaSet {
	var set schemaSet
	if err := json.Unmarshal(schemaJSON, &set); err != nil {
		panic("invalid checked-in management schema")
	}
	return set
}

// LookupAction returns metadata only. There is no generic execute fallback.
func LookupAction(id string) (ActionDescriptor, bool) {
	for _, descriptor := range contract.Actions {
		if descriptor.ID == id {
			return descriptor, true
		}
	}
	return ActionDescriptor{}, false
}

func KnownReason(reason ReasonCode) bool {
	for _, known := range contract.Reasons {
		if string(reason) == known {
			return true
		}
	}
	return false
}

func EffectiveReason(reason ReasonCode) ReasonCode {
	if !KnownReason(reason) {
		return "UNKNOWN"
	}
	return reason
}

// Accepted never promotes an unfamiliar result/reason into success.
func Accepted(result CommandResult) bool {
	return result.ContractVersion == Version && result.Status == "ACCEPTED" &&
		(result.Reason == "ACCEPTED" || result.Reason == "ALREADY_EXISTS" || result.Reason == "IN_PROGRESS")
}

// Decode drops compatible unknown descriptive fields before typed decoding.
// This avoids encoding/json's case-insensitive struct matching turning an
// ignored field such as Status into authority over the known status field.
func Decode(name string, data []byte, target any) error {
	value, err := parseJSON(data)
	if err != nil {
		return err
	}
	definition, exists := contract.Definitions[name]
	if !exists {
		return errors.New("unknown contract document")
	}
	if err := validate(definition, value, name, 0); err != nil {
		return err
	}
	if err := semantic(name, value); err != nil {
		return err
	}
	cleaned, err := json.Marshal(project(definition, value))
	if err != nil {
		return err
	}
	return json.Unmarshal(cleaned, target)
}

func ValidateDocument(name string, data []byte) error {
	var value json.RawMessage
	return Decode(name, data, &value)
}

func DecodeCommand(data []byte) (Command, error) {
	var command Command
	if err := Decode("Command", data, &command); err != nil {
		return Command{}, err
	}
	if err := ValidateCommand(command); err != nil {
		return Command{}, err
	}
	return command, nil
}

// ValidateCommand binds a closed, typed input to the existing semantic Action.
// It does not authorize it. ExpectedRevision is independent from commandId.
func ValidateCommand(command Command) error {
	data, err := json.Marshal(command)
	if err != nil || ValidateDocument("Command", data) != nil {
		return errors.New("INVALID_INPUT")
	}
	descriptor, known := LookupAction(command.ActionID)
	if !known || command.InputSchema != descriptor.InputSchema {
		return errors.New("UNSUPPORTED")
	}
	if command.Host != command.Resource.Host || command.Resource.Owner != descriptor.Owner {
		return errors.New("PERMISSION_DENIED")
	}
	if err := ValidateDocument(contract.Inputs[command.ActionID], command.Input); err != nil {
		return errors.New("INVALID_INPUT")
	}
	var ownerInput string
	switch command.ActionID {
	case "backup.restore":
		var input RestoreInput
		if err := Decode("RestoreInput", command.Input, &input); err != nil {
			return errors.New("INVALID_INPUT")
		}
		ownerInput = string(input.RehearsalRevision)
		if command.Resource.ResourceID != "database" {
			return errors.New("PERMISSION_DENIED")
		}
		if command.ExpectedRevision == nil || string(*command.ExpectedRevision) != ownerInput {
			return errors.New("INVALID_INPUT")
		}
	case "component.remove":
		var input ComponentRemoveInput
		if err := Decode("ComponentRemoveInput", command.Input, &input); err != nil {
			return errors.New("INVALID_INPUT")
		}
		ownerInput = input.ComponentID
		if command.Resource.ResourceID != "component:"+ownerInput {
			return errors.New("PERMISSION_DENIED")
		}
	default:
		return errors.New("UNSUPPORTED")
	}
	if string(command.InputDigest) != OwnerInputDigest(command.InputSchema, ownerInput) {
		return errors.New("INVALID_INPUT")
	}
	return nil
}

// OwnerInputDigest retains R5's schema+NUL+typed-input identity, byte for byte.
func OwnerInputDigest(schema, input string) string {
	return hash([]byte(schema + "\x00" + input))
}

// CommandDigest is the v1 ASCII/integer canonical semantic payload digest.
// Delivery identity, correlation and grant reference are deliberately excluded:
// a retry may carry freshly validated authority without becoming a new mutation.
// All execution-affecting scope, generation, fence and typed input are included.
func CommandDigest(command Command) (string, error) {
	if err := ValidateCommand(command); err != nil {
		return "", err
	}
	input, err := parseJSON(command.Input)
	if err != nil {
		return "", err
	}
	value := map[string]any{"contractVersion": command.ContractVersion, "actionId": command.ActionID,
		"host": command.Host, "resource": command.Resource, "generation": command.Generation,
		"inputSchema": command.InputSchema, "inputDigest": command.InputDigest, "input": input}
	if command.ExpectedRevision != nil {
		value["expectedRevision"] = *command.ExpectedRevision
	}
	// Reparse after typed marshaling so nested objects use the same sorted-key
	// encoding. No map iteration order is observable in the canonical bytes.
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	canonical, err := parseJSON(data)
	if err != nil {
		return "", err
	}
	data, err = json.Marshal(canonical)
	return hash(data), err
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func parseJSON(data []byte) (any, error) {
	if len(data) == 0 || len(data) > MaxDocumentBytes || !utf8.Valid(data) {
		return nil, errors.New("invalid contract document size or UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := readValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing contract document")
	}
	return value, nil
}

func readValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > 24 {
		return nil, errors.New("contract nesting exceeds bound")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			object := map[string]any{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if _, duplicate := object[name]; !ok || duplicate || len(object) >= 48 {
					return nil, errors.New("invalid or duplicate contract field")
				}
				child, err := readValue(decoder, depth+1)
				if err != nil {
					return nil, err
				}
				object[name] = child
			}
			_, err = decoder.Token()
			return object, err
		case '[':
			array := []any{}
			for decoder.More() {
				if len(array) >= 256 {
					return nil, errors.New("contract array exceeds bound")
				}
				child, err := readValue(decoder, depth+1)
				if err != nil {
					return nil, err
				}
				array = append(array, child)
			}
			_, err = decoder.Token()
			return array, err
		}
		return nil, errors.New("unexpected contract delimiter")
	case json.Number:
		integer, err := value.Int64()
		if err != nil || integer > maxInteger || integer < -maxInteger {
			return nil, errors.New("contract requires exact bounded integers")
		}
		return value, nil
	case string, bool, nil:
		return value, nil
	default:
		return nil, errors.New("invalid contract value")
	}
}

// validate implements only the assertions used by the checked-in schema.
// generate.mjs rejects all other assertion keywords before freezing a bundle.
func validate(rule map[string]any, value any, at string, depth int) error {
	bad := func() error { return fmt.Errorf("invalid contract field %s", at) }
	if depth > 32 {
		return bad()
	}
	if reference, ok := rule["$ref"].(string); ok {
		name := strings.TrimPrefix(reference, "#/$defs/")
		definition, exists := contract.Definitions[name]
		if !exists || validate(definition, value, at, depth+1) != nil {
			return bad()
		}
		return semantic(name, value)
	}
	if expected, exists := rule["const"]; exists && expected != value {
		return bad()
	}
	if choices, ok := rule["enum"].([]any); ok {
		found := false
		for _, choice := range choices {
			if choice == value {
				found = true
			}
		}
		if !found {
			return bad()
		}
	}
	if choices, ok := rule["oneOf"].([]any); ok {
		matched := 0
		for _, choice := range choices {
			if validate(choice.(map[string]any), value, at, depth+1) == nil {
				matched++
			}
		}
		if matched != 1 {
			return bad()
		}
	}
	switch rule["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok || exceeds(rule, "maxProperties", len(object)) {
			return bad()
		}
		properties, _ := rule["properties"].(map[string]any)
		for _, required := range rule["required"].([]any) {
			if _, exists := object[required.(string)]; !exists {
				return bad()
			}
		}
		for key, child := range object {
			property, exists := properties[key]
			if !exists {
				if allowed, present := rule["additionalProperties"].(bool); present && !allowed {
					return bad()
				}
				continue
			}
			if err := validate(property.(map[string]any), child, at+"."+key, depth+1); err != nil {
				return err
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok || exceeds(rule, "maxItems", len(array)) || below(rule, "minItems", len(array)) {
			return bad()
		}
		for _, child := range array {
			if err := validate(rule["items"].(map[string]any), child, at+"[]", depth+1); err != nil {
				return err
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok || exceeds(rule, "maxLength", utf8.RuneCountInString(text)) || below(rule, "minLength", utf8.RuneCountInString(text)) {
			return bad()
		}
		if pattern, ok := rule["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(text) {
			return bad()
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return bad()
		}
		integer, err := number.Int64()
		if err != nil || float64(integer) < rule["minimum"].(float64) || float64(integer) > rule["maximum"].(float64) {
			return bad()
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return bad()
		}
	}
	return nil
}

func exceeds(rule map[string]any, key string, size int) bool {
	limit, exists := rule[key].(float64)
	return exists && float64(size) > limit
}
func below(rule map[string]any, key string, size int) bool {
	limit, exists := rule[key].(float64)
	return exists && float64(size) < limit
}

func project(rule map[string]any, value any) any {
	if reference, ok := rule["$ref"].(string); ok {
		return project(contract.Definitions[strings.TrimPrefix(reference, "#/$defs/")], value)
	}
	if rule["type"] == "object" {
		result := map[string]any{}
		properties, _ := rule["properties"].(map[string]any)
		for key, child := range value.(map[string]any) {
			if property, exists := properties[key]; exists {
				result[key] = project(property.(map[string]any), child)
			}
		}
		return result
	}
	if rule["type"] == "array" {
		result := []any{}
		for _, child := range value.([]any) {
			result = append(result, project(rule["items"].(map[string]any), child))
		}
		return result
	}
	return value
}

func semantic(name string, value any) error {
	invalid := errors.New("invalid " + name + " semantics")
	if name == "Generation" {
		_, err := strconv.ParseUint(value.(string), 10, 64)
		return err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	switch name {
	case "NodeIdentity":
		host := object["host"].(map[string]any)
		if host["kind"] != "NODE" || host["id"] != object["nodeId"] {
			return invalid
		}
		_, authority := object["authorityRef"]
		_, generation := object["authorizationGeneration"]
		if object["enrollmentState"] == "UNENROLLED" {
			if authority || generation {
				return invalid
			}
		} else if !authority || !generation {
			return invalid
		}
	case "Progress":
		completed, _ := object["completed"].(json.Number).Int64()
		total, _ := object["total"].(json.Number).Int64()
		if completed > total {
			return invalid
		}
	case "NetworkObservation":
		observed, _ := object["observedAt"].(json.Number).Int64()
		until, _ := object["freshUntil"].(json.Number).Int64()
		if until < observed {
			return invalid
		}
	case "HealthEvidence":
		if object["evidenceRef"] != "health:"+object["evidenceRevision"].(string) {
			return invalid
		}
	case "ArtifactIdentity":
		if object["artifactId"] != "artifact:"+hash([]byte(object["kind"].(string)+"\x00"+object["contentDigest"].(string))) {
			return invalid
		}
	case "Component":
		if object["state"] == "ENABLED_HEALTHY" && object["evidenceRef"] == nil {
			return invalid
		}
	}
	return nil
}
