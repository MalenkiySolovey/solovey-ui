package managementv1

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// These exact checked-in vectors and tests travel with the pinned bundle.
// State-machine vectors are also run by the owner adapters and Node teststub.
func TestSchemaVectors(t *testing.T) {
	data, err := os.ReadFile("vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Version string `json:"version"`
		Cases   []struct {
			ID             string          `json:"id"`
			Kind           string          `json:"kind"`
			Type           string          `json:"type"`
			Document       json.RawMessage `json:"document"`
			Valid          bool            `json:"valid"`
			First          Command         `json:"first"`
			Second         Command         `json:"second"`
			Equal          bool            `json:"equal"`
			ExpectedFirst  string          `json:"expectedFirst"`
			ExpectedSecond string          `json:"expectedSecond"`
			Result         CommandResult   `json:"result"`
			Accepted       bool            `json:"accepted"`
			Reason         ReasonCode      `json:"reason"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil || vectors.Version != "solovey.management.conformance/v1" {
		t.Fatalf("invalid vector set: %v", err)
	}
	count := 0
	for _, vector := range vectors.Cases {
		if vector.Kind != "document" && vector.Kind != "digest" && vector.Kind != "reason" {
			continue
		}
		count++
		t.Run(vector.ID, func(t *testing.T) {
			switch vector.Kind {
			case "document":
				var output json.RawMessage
				err := Decode(vector.Type, vector.Document, &output)
				if (err == nil) != vector.Valid {
					t.Fatalf("valid=%v: %v", vector.Valid, err)
				}
				if err == nil {
					if err := ValidateDocument(vector.Type, output); err != nil {
						t.Fatalf("round-trip: %v", err)
					}
				}
			case "digest":
				first, err := CommandDigest(vector.First)
				if err != nil {
					t.Fatal(err)
				}
				second, err := CommandDigest(vector.Second)
				if err != nil || (first == second) != vector.Equal {
					t.Fatalf("digest equality=%v: %v", first == second, err)
				}
				if first != vector.ExpectedFirst || second != vector.ExpectedSecond {
					t.Fatalf("canonical digest differs from independent JSON golden: %s / %s", first, second)
				}
			case "reason":
				if Accepted(vector.Result) != vector.Accepted || EffectiveReason(vector.Result.Reason) != vector.Reason {
					t.Fatal("reason evolution changed success semantics")
				}
			}
		})
	}
	if count < 35 {
		t.Fatal("missing schema vectors")
	}
}

func TestMalformedInputDeniesWithoutPanic(t *testing.T) {
	for _, raw := range []string{
		``, `null`, `[]`, `{}`, `{"kind":"LOCAL","kind":"NODE","id":"local"}`,
		`{"kind":"LOCAL","id":"local"} {}`, `{"kind":{},"id":"local"}`,
		`{"kind":null,"id":"local"}`, `{"Kind":"LOCAL","id":"local"}`,
		`{"kind":"LOCAL","id":"local","extra":true}`, `{"kind":"LOCAL","id":" local"}`,
		`{"kind":"LOCAL","id":"\ud800"}`, `{"kind":"LOCAL","id":1e0}`,
		`{"kind":"LOCAL","id":9007199254740992}`, string([]byte{'{', '"', 0xff}),
	} {
		t.Run(strconv.Quote(raw), func(t *testing.T) {
			if ValidateDocument("HostRef", []byte(raw)) == nil {
				t.Fatal("malformed identity accepted")
			}
		})
	}
	if ValidateDocument("Progress", []byte(`{"completed":1.5,"total":2,"unit":"bytes"}`)) == nil ||
		ValidateDocument("Progress", []byte(`{"completed":0,"total":0,"unit":"bytes"}`)) == nil ||
		ValidateDocument("Progress", []byte(`{"completed":-1,"total":2,"unit":"bytes"}`)) == nil {
		t.Fatal("invalid progress became a valid projection")
	}
	if ValidateDocument("Identifier", []byte(strconv.Quote(strings.Repeat("a", 257)))) == nil ||
		ValidateDocument("Identifier", []byte(`"/tmp/identity"`)) == nil ||
		ValidateDocument("Identifier", []byte(strconv.Quote(strings.Repeat("a", MaxDocumentBytes)))) == nil {
		t.Fatal("identifier or document bounds were lost")
	}
}

func TestCompatibleFieldsCannotOverrideKnownFields(t *testing.T) {
	var result CommandResult
	data, err := os.ReadFile("vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var set struct{ Cases []map[string]json.RawMessage }
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatal(err)
	}
	for _, vector := range set.Cases {
		if string(vector["id"]) != `"read-unknown-compatible-field"` {
			continue
		}
		if err := Decode("CommandResult", vector["document"], &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != "REJECTED" || Accepted(result) {
			t.Fatal("ignored Status field overrode status")
		}
		return
	}
	t.Fatal("missing compatible-field vector")
}

func TestPublicContractDependsOnlyOnStandardLibrary(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".go") {
			continue
		}
		source, err := parser.ParseFile(token.NewFileSet(), file.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range source.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil || strings.Contains(strings.Split(name, "/")[0], ".") {
				t.Fatalf("public contract private/external dependency: %s", imported.Path.Value)
			}
			if !strings.HasSuffix(file.Name(), "_test.go") && (name == "net" || strings.HasPrefix(name, "net/") || name == "os/exec" || name == "database/sql" || name == "syscall") {
				t.Fatalf("public contract gained execution/transport coupling: %s", name)
			}
		}
	}
	for name, definition := range contract.Definitions {
		if strings.HasPrefix(name, "Remote") || name == "NodeInbound" || name == "NodeOutbound" || name == "NodeDNS" || name == "NodeRoute" {
			t.Fatalf("duplicate semantic identity %s", name)
		}
		properties, _ := definition["properties"].(map[string]any)
		for _, forbidden := range []string{"shell", "sql", "path", "script", "runtime", "execute", "httpStatus", "grpcStatus"} {
			if _, exists := properties[forbidden]; exists {
				t.Fatalf("implementation/transport field %s.%s", name, forbidden)
			}
		}
	}
}

func TestCatalogIsRepresentableByItsOwnSchema(t *testing.T) {
	for _, descriptor := range contract.Actions {
		data, err := json.Marshal(descriptor)
		if err != nil || ValidateDocument("ActionDescriptor", data) != nil {
			t.Fatalf("unrepresentable action descriptor %s: %s", descriptor.ID, data)
		}
	}
}

func TestCommandVectorShapeAndOwnerBindings(t *testing.T) {
	data, err := os.ReadFile("vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Cases []struct {
			ID, Kind string
			Steps    []struct{ Command json.RawMessage }
		}
	}
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatal(err)
	}
	for _, vector := range set.Cases {
		if vector.Kind != "command" {
			continue
		}
		t.Run(vector.ID, func(t *testing.T) {
			for _, step := range vector.Steps {
				_, err := DecodeCommand(step.Command)
				expected := ""
				if vector.ID == "unknown-action" {
					expected = "UNSUPPORTED"
				}
				if vector.ID == "foreign-command-resource" {
					expected = "PERMISSION_DENIED"
				}
				if expected == "" && err != nil || expected != "" && (err == nil || err.Error() != expected) {
					t.Fatalf("expected %q, got %v", expected, err)
				}
			}
		})
	}
}
