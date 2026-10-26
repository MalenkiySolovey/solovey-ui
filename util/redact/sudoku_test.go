package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSudokuPrivateMaterialRedactedWithoutHidingPublicKeys(t *testing.T) {
	type sudoku struct {
		Keys   map[string]string `json:"keys"`
		Public string            `json:"key"`
	}
	type config struct {
		Sudoku sudoku   `json:"sudoku"`
		Keys   []string `json:"keys"`
	}
	for name, input := range map[string]any{
		"replace":   map[string]any{"master_key": map[string]any{"intent": "replace", "value": "master-canary"}, "sudoku": map[string]any{"keys": map[string]any{"intent": "replace", "value": `{"7":"delivery-canary"}`}, "key": "public-serving-key"}, "keys": []string{"public-metadata"}},
		"raw JSON":  json.RawMessage(`{"master_key":"master-canary","sudoku":{"keys":"{\"7\":\"delivery-canary\"}","key":"public-serving-key"},"keys":["public-metadata"]}`),
		"typed":     config{Sudoku: sudoku{Keys: map[string]string{"7": "delivery-canary"}, Public: "public-serving-key"}, Keys: []string{"public-metadata"}},
		"typed map": map[string]map[string]string{"sudoku": {"keys": "delivery-canary", "key": "public-serving-key"}, "metadata": {"keys": "public-metadata"}},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(Value(input))
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"master-canary", "delivery-canary"} {
				if strings.Contains(string(raw), secret) {
					t.Fatal("Sudoku private material reached a structured sink")
				}
			}
			for _, public := range []string{"public-serving-key", "public-metadata"} {
				if !strings.Contains(string(raw), public) {
					t.Fatal("redaction hid unrelated public metadata")
				}
			}
		})
	}
	for _, input := range []string{"master_key=master-canary", "master-key: master-canary"} {
		if strings.Contains(String(input), "master-canary") {
			t.Fatal("master material reached an unstructured sink")
		}
	}
}
