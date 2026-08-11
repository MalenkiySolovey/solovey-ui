package fronting

import (
	"bytes"
	"fmt"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func TestSelectorV1ExactTokenMembershipPrecedenceAndAmbiguity(t *testing.T) {
	first, second := strings.Repeat("1", 64), strings.Repeat("2", 64)
	set, err := CanonicalizeSelectorSetV1([]SelectorRouteInputV1{
		{SNI: "route.example", ALPN: []string{"h2", "http/1.1"}, TargetReferenceRevision: first},
		{SNI: "route.example", TargetReferenceRevision: second},
	}, SelectorDefaultV1{})
	if err != nil || set.ALPNSemantics != SelectorALPNExactTokenMembershipV1 {
		t.Fatalf("set=%#v err=%v", set, err)
	}
	tests := []struct {
		name, sni, alpn, class, target string
		rejected                       bool
	}{
		{"alpn", "route.example", "h2", "SNI_ALPN", first, false},
		{"token-boundary", "route.example", "http/1.1,h2", "SNI_ALPN", first, false},
		{"substring", "route.example", "h2c", "SNI_ONLY", second, false},
		{"prefix", "route.example", "x-h2", "SNI_ONLY", second, false},
		{"case", "route.example", "H2", "SNI_ONLY", second, false},
		{"sni-only", "route.example", "", "SNI_ONLY", second, false},
		{"unknown", "unknown.example", "h2", "REJECT", "", true},
		{"missing", "", "", "REJECT", "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ResolveFiniteSelectorV2(set, test.sni, test.alpn)
			if got.Class != test.class || got.TargetReferenceRevision != test.target || got.Rejected != test.rejected {
				t.Fatalf("resolution=%#v", got)
			}
		})
	}
	if _, err := CanonicalizeSelectorSetV1([]SelectorRouteInputV1{
		{SNI: "route.example", ALPN: []string{"h2"}, TargetReferenceRevision: first},
		{SNI: "route.example", ALPN: []string{"http/1.1"}, TargetReferenceRevision: second},
	}, SelectorDefaultV1{}); err == nil || err.Error() != "selector_alpn_target_ambiguous" {
		t.Fatalf("ambiguous multi-ALPN targets were accepted: %v", err)
	}
	if _, err := CanonicalizeSelectorSetV1([]SelectorRouteInputV1{
		{SNI: "Route.Example", ALPN: []string{"h2"}, TargetReferenceRevision: first},
		{SNI: "route.example", TargetReferenceRevision: second},
	}, SelectorDefaultV1{}); err == nil || err.Error() != "selector_sni_canonical_collision" {
		t.Fatalf("case-canonical collision was accepted: %v", err)
	}
}

func TestSNIPlannerBlocksALPNBecauseNginxProjectionCannotProveTokenBoundaries(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	input := l4PlanInputV2(t, now, true)
	input.DesiredStrategy = StrategySNIPreread
	target := input.BackendReferences[0].CanonicalReferenceRevision
	input.Selectors, _ = CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: "route.example", ALPN: []string{"h2"}, TargetReferenceRevision: target}}, SelectorDefaultV1{})
	plan, err := PlanFrontingStrategyV2(input)
	if err != nil || plan.Strategy.Selected != "" || !containsReasonV2(plan.Safety.ReasonCodes, "alpn_exact_projection_unavailable") {
		t.Fatalf("lossy ALPN projection was not blocked: plan=%#v err=%v", plan, err)
	}
	plan.Strategy.Selected, plan.Safety.Projection.Selected = StrategySNIPreread, StrategySNIPreread
	plan.Safety.Blocks, plan.Safety.ReasonCodes = nil, nil
	plan = rehashPlanV2(plan)
	authorities := map[string]string{target: v2Revision("lease")}
	if _, err := RenderSNIPrereadCandidateV2(plan, input, authorities, now); err == nil {
		t.Fatal("direct renderer accepted an ALPN selector")
	}
}

func TestSNIPrereadCandidateMaximumShapePerformanceInvariants(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	input := largePlanInputV2(t, now)
	if len(input.Selectors.Tuples) != MaxFrontingRoutesV1 || len(input.Selectors.TargetRevisions) != MaxFrontingRoutesV1 {
		t.Fatalf("maximum fixture has tuples=%d targets=%d", len(input.Selectors.Tuples), len(input.Selectors.TargetRevisions))
	}
	plan, err := PlanFrontingStrategyV2(input)
	if err != nil || plan.Strategy.Selected != StrategySNIPreread || len(plan.Safety.Blocks) != 0 {
		t.Fatalf("maximum plan=%#v err=%v", plan, err)
	}
	authorities := make(map[string]string, len(plan.Selectors.TargetRevisions))
	for index, revision := range plan.Selectors.TargetRevisions {
		authorities[revision] = v2Revision(struct{ Lease int }{index})
	}
	before := runtime.NumGoroutine()
	var candidate SNIPrereadCandidateV2
	for range 31 {
		candidate, err = RenderSNIPrereadCandidateV2(plan, input, authorities, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	allocations := testing.AllocsPerRun(20, func() {
		_, _ = RenderSNIPrereadCandidateV2(plan, input, authorities, now)
	})
	if len(candidate.Bytes) > MaxFutureCandidateBytesV1 || len(candidate.Targets) != MaxFrontingRoutesV1 || runtime.NumGoroutine() != before {
		t.Fatalf("candidate invariants tuples=%d targets=%d bytes=%d allocations=%.0f goroutines=%d->%d", len(plan.Selectors.Tuples), len(candidate.Targets), len(candidate.Bytes), allocations, before, runtime.NumGoroutine())
	}
	t.Logf("SNI candidate invariants tuples=%d targets=%d bytes=%d allocations=%.0f goroutine_delta=%d", len(plan.Selectors.Tuples), len(candidate.Targets), len(candidate.Bytes), allocations, runtime.NumGoroutine()-before)
}

func TestSNIPrereadCandidateFiniteMapAndSafetyRevisions(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	input := sniPlanInputV2(t, now, SelectorDefaultV1{})
	plan, err := PlanFrontingStrategyV2(input)
	if err != nil || len(plan.Safety.Blocks) != 0 || plan.Strategy.Selected != StrategySNIPreread {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	authorities := map[string]string{}
	for index, revision := range plan.Selectors.TargetRevisions {
		authorities[revision] = v2Revision(struct{ Lease int }{index})
	}
	candidate, err := RenderSNIPrereadCandidateV2(plan, input, authorities, now)
	if err != nil {
		t.Fatal(err)
	}
	text := string(candidate.Bytes)
	for _, wanted := range []string{"ssl_preread on;", "map $ssl_preread_server_name $solovey_selected_upstream", "proxy_pass $solovey_selected_upstream;", "server 127.0.0.1:1 down;"} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("candidate lacks %q:\n%s", wanted, text)
		}
	}
	for _, forbidden := range []string{"resolver ", "ssl_certificate", "proxy_ssl", "proxy_pass $ssl_preread_server_name", "proxy_pass $ssl_preread_alpn_protocols", "backup", "proxy_next_upstream", "include ", "http {", "location ", "listen udp", "unix:"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("candidate contains forbidden %q:\n%s", forbidden, text)
		}
	}
	if len(candidate.Bytes) > MaxFutureCandidateBytesV1 || len(candidate.Targets) != 2 || candidate.MapRevision == "" || candidate.UpstreamIDSetRevision == "" {
		t.Fatalf("candidate bounds or bindings are incomplete: %#v", candidate)
	}
	again, err := RenderSNIPrereadCandidateV2(plan, input, authorities, now)
	if err != nil || candidate.Revision != again.Revision || candidate.SHA256 != again.SHA256 || string(candidate.Bytes) != string(again.Bytes) {
		t.Fatal("SNI candidate is not deterministic")
	}
	changed := cloneAuthorityRevisionsV2(authorities)
	changed[plan.Selectors.TargetRevisions[0]] = v2Revision("changed-lease")
	next, err := RenderSNIPrereadCandidateV2(plan, input, changed, now)
	if err != nil || next.Revision == candidate.Revision || next.MapRevision != candidate.MapRevision {
		t.Fatalf("lease revision did not change only the candidate safety revision: before=%s after=%s err=%v", candidate.Revision, next.Revision, err)
	}
}

func TestSNIPrereadCandidateValidationRejectsRehashedBindingAndFiniteDomainTamper(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	input := sniPlanInputV2(t, now, SelectorDefaultV1{})
	plan, err := PlanFrontingStrategyV2(input)
	if err != nil {
		t.Fatal(err)
	}
	authorities := map[string]string{}
	for index, revision := range plan.Selectors.TargetRevisions {
		authorities[revision] = v2Revision(struct{ Lease int }{index})
	}
	candidate, err := RenderSNIPrereadCandidateV2(plan, input, authorities, now)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("binding", func(t *testing.T) {
		tampered := candidate
		tampered.CanonicalInput = append([]byte(nil), candidate.CanonicalInput...)
		tampered.CanonicalInput = bytes.Replace(tampered.CanonicalInput, []byte(sniPrereadTimeoutV2), []byte("6s"), 1)
		if err := ValidateSNIPrereadCandidateV2(tampered, plan); err == nil {
			t.Fatal("tampered canonical safety binding was accepted")
		}
	})
	t.Run("map-output", func(t *testing.T) {
		tampered := candidate
		tampered.Bytes = bytes.Replace(append([]byte(nil), candidate.Bytes...), []byte("  default "+candidate.RejectID+";"), []byte("  default unexpected_upstream;"), 1)
		tampered.SHA256 = fixedL4DigestV2(tampered.Bytes)
		if err := ValidateSNIPrereadCandidateV2(tampered, plan); err == nil {
			t.Fatal("rehashed out-of-domain map output was accepted")
		}
	})
	t.Run("upstream-set", func(t *testing.T) {
		tampered := candidate
		tampered.Bytes = append(append([]byte(nil), candidate.Bytes...), []byte("\nupstream "+candidate.RejectID+" { server 127.0.0.1:2; }\n")...)
		tampered.SHA256 = fixedL4DigestV2(tampered.Bytes)
		if err := ValidateSNIPrereadCandidateV2(tampered, plan); err == nil {
			t.Fatal("rehashed duplicate generated upstream was accepted")
		}
	})
	t.Run("hostname-upstream", func(t *testing.T) {
		tampered := candidate
		first := candidate.Targets[0]
		exact := []byte("  server " + net.JoinHostPort(first.Address, fmt.Sprint(first.Port)) + ";")
		tampered.Bytes = bytes.Replace(append([]byte(nil), candidate.Bytes...), exact, []byte("  server backend.example:443;"), 1)
		tampered.SHA256 = fixedL4DigestV2(tampered.Bytes)
		if err := ValidateSNIPrereadCandidateV2(tampered, plan); err == nil {
			t.Fatal("rehashed hostname upstream was accepted")
		}
	})
}

func TestSNIPrereadDefaultPoliciesKeepEmptyPrereadRejected(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	input := sniPlanInputV2(t, now, SelectorDefaultV1{})
	defaultRevision := input.BackendReferences[1].CanonicalReferenceRevision
	input.Selectors, _ = CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: "route.example", TargetReferenceRevision: input.BackendReferences[0].CanonicalReferenceRevision}},
		SelectorDefaultV1{Policy: SelectorDefaultFixedSafe, TargetReferenceRevision: defaultRevision})
	plan, err := PlanFrontingStrategyV2(input)
	if err != nil || len(plan.Safety.Blocks) != 0 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	unknown := ResolveFiniteSelectorV2(plan.Selectors, "unknown.example", "h2")
	empty := ResolveFiniteSelectorV2(plan.Selectors, "", "")
	overlong := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 63)
	malformed := ResolveFiniteSelectorV2(plan.Selectors, overlong, "")
	if unknown.TargetReferenceRevision != defaultRevision || unknown.Class != "FIXED_SAFE_DEFAULT" || empty.Class != "REJECT" || !empty.Rejected || malformed.Class != "REJECT" || !malformed.Rejected {
		t.Fatalf("unknown=%#v empty=%#v malformed=%#v", unknown, empty, malformed)
	}
	input.Selectors, _ = CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: "route.example", TargetReferenceRevision: input.BackendReferences[0].CanonicalReferenceRevision}},
		SelectorDefaultV1{Policy: SelectorDefaultNonTLSFixedTarget, TargetReferenceRevision: defaultRevision})
	blocked, err := PlanFrontingStrategyV2(input)
	if err != nil || !containsReasonV2(blocked.Safety.ReasonCodes, "non_tls_discriminator_unproven") || blocked.Strategy.Selected != "" {
		t.Fatalf("non-TLS default was not blocked: %#v err=%v", blocked, err)
	}
}

func sniPlanInputV2(t *testing.T, now time.Time, policy SelectorDefaultV1) FrontingPlanInputV2 {
	t.Helper()
	input := l4PlanInputV2(t, now, true)
	input.DesiredStrategy = StrategySNIPreread
	second := backendFactV2(t, now, 1)
	secondReference, err := hostresources.ReferenceFrontingBackendV1(second, hostresources.ProxyModeOff, now)
	if err != nil {
		t.Fatal(err)
	}
	input.Inventory = append(input.Inventory, second)
	input.BackendReferences = append(input.BackendReferences, secondReference)
	input.Selectors, err = CanonicalizeSelectorSetV1([]SelectorRouteInputV1{
		{SNI: "route.example", TargetReferenceRevision: input.BackendReferences[0].CanonicalReferenceRevision},
		{SNI: "alternate.example", TargetReferenceRevision: secondReference.CanonicalReferenceRevision},
	}, policy)
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func cloneAuthorityRevisionsV2(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
