package validation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/route/rule"
)

func TestImmutableBaselineRuleUpgrade(t *testing.T) {
	dir := filepath.Join("testdata", "nested_rules_v2026.3.3")
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name            string          `json:"name"`
		File            string          `json:"file"`
		SHA             string          `json:"sha256"`
		Commit          string          `json:"solovey_commit"`
		Core            string          `json:"sing_box"`
		StoreAccepted   bool            `json:"store_accepted"`
		DecoderAccepted bool            `json:"decoder_accepted"`
		Matches         map[string]bool `json:"matches"`
		Action          string          `json:"action"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 16 {
		t.Fatal("baseline capture incomplete")
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			if f.Commit != "3523eeea9f91c091ab3f0fbdb94c3955e37798b4" || f.Core != "v1.13.18" || !f.StoreAccepted || !f.DecoderAccepted {
				t.Fatal("not accepted immutable old state")
			}
			source, err := os.ReadFile(filepath.Join(dir, f.File))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(source)
			if hex.EncodeToString(sum[:]) != f.SHA {
				t.Fatal("old-state bytes changed")
			}
			preimage := bytes.Clone(source)
			var opt option.Options
			if opt.UnmarshalJSONContext(context.Background(), source) == nil {
				t.Fatal("raw legacy state silently accepted by target")
			}
			findings, err := ValidateRuleConditions(source)
			var typed *RuleConditionError
			if !errors.As(err, &typed) || typed.Finding.Code != "nested_rule_action" || len(findings) == 0 {
				t.Fatal("new saves must use stable rejection", err)
			}
			result, err := PrepareRuleUpgrade(source)
			if !bytes.Equal(source, preimage) || !bytes.Equal(result.Original, preimage) {
				t.Fatal("upgrade destroyed pre-image")
			}
			if len(f.Matches) == 0 {
				if err == nil || result.Outcome != RuleManualRequired || !bytes.Equal(result.Candidate, preimage) {
					t.Fatal("unproved state was guessed", result.Outcome, err)
				}
				if !errors.As(err, &typed) || !typed.Finding.OperatorActionRequired || typed.Finding.AutomaticAvailable {
					t.Fatal("manual diagnostic is not actionable")
				}
				return
			}
			if err != nil || result.Outcome != RuleAutomaticDiagnostic {
				t.Fatal("proven shape did not migrate", err)
			}
			if len(result.Findings) == 0 || !result.Findings[0].AutomaticAvailable || result.Findings[0].OperatorActionRequired {
				t.Fatal("automatic conversion was silent")
			}
			if _, err := ValidateRuleConditions(result.Candidate); err != nil {
				t.Fatal(err)
			}
			if err := opt.UnmarshalJSONContext(context.Background(), result.Candidate); err != nil {
				t.Fatal(err)
			}
			var r adapter.Rule
			logger := log.NewNOPFactory().NewLogger("fixture")
			if strings.HasPrefix(f.Name, "route-") {
				r, err = rule.NewRule(context.Background(), logger, opt.Route.Rules[0], true)
			} else {
				r, err = rule.NewDNSRule(context.Background(), logger, opt.DNS.Rules[0], true, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			for domain, oldMatch := range f.Matches {
				m := adapter.InboundContext{Domain: domain}
				if r.Match(&m) != oldMatch {
					t.Fatalf("matching changed for %q", domain)
				}
			}
			if r.Action().Type() != f.Action {
				t.Fatal("termination action changed")
			}
			if c, ok := r.(interface{ Close() error }); ok {
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			}
			twice, err := PrepareRuleUpgrade(result.Candidate)
			if err != nil || !bytes.Equal(twice.Candidate, result.Candidate) || twice.Outcome != RuleLosslessAutomatic {
				t.Fatal("migration is not idempotent")
			}
			var roundtrip any
			if err := json.Unmarshal(result.Candidate, &roundtrip); err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(roundtrip)
			if _, err := ValidateRuleConditions(encoded); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRuleCompatibilityTopLevelAndNestedGrammar(t *testing.T) {
	for _, kind := range []string{"route", "dns"} {
		for _, input := range []string{
			`{"rules":[{"domain":"fixture.example","action":"reject"}]}`,
			`{"rules":[{"type":"logical","mode":"or","action":"reject","rules":[{"domain":"fixture.example"},{"domain_suffix":"example","invert":true}]}]}`,
		} {
			if _, err := ValidateRuleConditions([]byte(`{"` + kind + `":` + input + `}`)); err != nil {
				t.Fatal(err)
			}
		}
		for _, input := range []string{
			`{"rules":[{"type":"logical","mode":"and","action":"reject","rules":[{"domain":"fixture.example","action":"secret-fixture-marker"}]}]}`,
			`{"rules":[{"type":"logical","mode":"or","action":"reject","rules":[{"domain":"fixture.example","outbound":"secret-fixture-marker"}]}]}`,
		} {
			data := []byte(`{"` + kind + `":` + input + `}`)
			if kind == "dns" {
				data = bytes.ReplaceAll(data, []byte("outbound"), []byte("server"))
			}
			if _, err := ValidateRuleConditions(data); err == nil {
				t.Fatalf("nested action escaped: %s", data)
			}
			result, err := PrepareRuleUpgrade(data)
			if err == nil || result.Outcome != RuleManualRequired || !bytes.Equal(result.Candidate, data) {
				t.Fatal("unknown action guessed")
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "secret-fixture-marker") {
				t.Fatal("diagnostic leaked submitted values")
			}
		}
	}
	// evaluate is top-level only; response matching never enters a legacy rewrite.
	data := []byte(`{"dns":{"rules":[{"type":"logical","mode":"or","action":"reject","rules":[{"domain":"fixture.example","action":"evaluate","server":"resolver","match_response":true}]}]}}`)
	result, err := PrepareRuleUpgrade(data)
	if err == nil || !bytes.Equal(result.Candidate, data) {
		t.Fatal("nested evaluate guessed")
	}
	var opt option.Options
	if opt.UnmarshalJSONContext(context.Background(), data) == nil {
		t.Fatal("nested evaluate must fail official decoding")
	}
}

func TestRuleUpgradeNeverPublishesPartialTree(t *testing.T) {
	data := []byte(`{"route":{"rules":[{"type":"logical","mode":"or","action":"reject","rules":[{"domain":"fixture.example","action":"reject"}]},{"type":"logical","mode":"and","action":"reject","rules":[{"domain":"fixture.example","action":"route"}]}]}}`)
	result, err := PrepareRuleUpgrade(data)
	if err == nil || result.Outcome != RuleManualRequired || !bytes.Equal(result.Candidate, data) || !bytes.Equal(result.Original, data) {
		t.Fatal("partial migration escaped")
	}
}

func TestRuleCompatibilityDNSResponseAndEvaluateBoundaries(t *testing.T) {
	data := []byte(`{"dns":{"rules":[{"domain":"fixture.example","action":"evaluate","server":"resolver","tag":"evaluation"},{"domain":"fixture.example","match_response":"evaluation","action":"respond"}]}}`)
	var opt option.Options
	if err := opt.UnmarshalJSONContext(context.Background(), data); err != nil {
		t.Fatal("top-level evaluate/response fixture is outside pinned grammar", err)
	}
	result, err := PrepareRuleUpgrade(data)
	if err != nil || result.Outcome != RuleLosslessAutomatic || !bytes.Equal(result.Candidate, data) {
		t.Fatal("top-level evaluate/response grammar changed", err)
	}
	data = []byte(`{"dns":{"rules":[{"type":"logical","mode":"and","action":"reject","rules":[{"domain":"fixture.example","match_response":true,"action":"reject"}]}]}}`)
	result, err = PrepareRuleUpgrade(data)
	if err == nil || result.Outcome != RuleManualRequired || !bytes.Equal(result.Candidate, data) {
		t.Fatal("response semantics entered redundant-action rewrite")
	}
}
