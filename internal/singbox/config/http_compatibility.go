package singboxconfig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

// HTTPCompatibility stages core HTTP state in its existing base-config owner.
// No application clients, files, transports or storage are opened here.
type HTTPCompatibility struct {
	Original  []byte                `json:"-"`
	Candidate []byte                `json:"-"`
	Outcome   string                `json:"outcome"`
	Findings  []diagnostics.Finding `json:"findings"`
}

func PrepareHTTPUpgrade(config []byte) (HTTPCompatibility, error) {
	return prepareHTTP(config, true, true)
}

// PrepareHTTPDownloads stages HTTP paths for an explicitly edited current
// candidate. Historical grouped-rule review remains owned by PrepareHTTPUpgrade.
func PrepareHTTPDownloads(config []byte) (HTTPCompatibility, error) {
	return prepareHTTP(config, true, false)
}
func ValidateHTTPConfig(config []byte) ([]diagnostics.Finding, error) {
	result, err := prepareHTTP(config, false, false)
	return result.Findings, err
}

func prepareHTTP(config []byte, historical, historicalGroups bool) (HTTPCompatibility, error) {
	result := HTTPCompatibility{Original: bytes.Clone(config), Candidate: bytes.Clone(config), Outcome: diagnostics.LosslessAutomatic}
	var root dnsObject
	if json.Unmarshal(config, &root) != nil || root == nil {
		result.Findings = append(result.Findings, httpFailure("config", "invalid_json", "Configuration must be an object."))
		return finishHTTP(result, nil, false)
	}
	var clients []dnsObject
	if raw, present := root["http_clients"]; present && (json.Unmarshal(raw, &clients) != nil || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("["))) {
		result.Findings = append(result.Findings, httpFailure("http_clients", "http_clients_invalid", "Shared HTTP clients must be an array of objects."))
		return finishHTTP(result, nil, false)
	}
	for i, client := range clients {
		path := fmt.Sprintf("http_clients[%d]", i)
		result.Findings = append(result.Findings, httpClientShape(client, path, true)...)
		result.Findings = append(result.Findings, httpOutboundReference(client, path, root)...)
		for j := 0; j < i; j++ {
			if dnsString(client, "tag") == dnsString(clients[j], "tag") {
				result.Findings = append(result.Findings, httpFailure(path+".tag", "http_client_tag_duplicate", "Shared HTTP client tags must be unique."))
				break
			}
		}
	}
	var route dnsObject
	if raw, present := root["route"]; present && (json.Unmarshal(raw, &route) != nil || route == nil) {
		result.Findings = append(result.Findings, httpFailure("route", "invalid_section", "Route must be an object."))
		return finishHTTP(result, nil, false)
	}
	if route == nil {
		route = dnsObject{}
	}
	defaultClient := dnsString(route, "default_http_client")
	if raw, present := route["default_http_client"]; present {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			result.Findings = append(result.Findings, httpFailure("route.default_http_client", "http_client_reference_invalid", "The default HTTP client must be a shared tag."))
		}
	}
	hadSharedDefault := len(clients) > 0 || defaultClient != ""
	if defaultClient != "" && !httpTagPresent(clients, defaultClient) {
		result.Findings = append(result.Findings, httpFailure("route.default_http_client", "http_client_reference_missing", "Select an existing shared HTTP client."))
	}
	result.Findings = append(result.Findings, RuleSetFindings(config, historicalGroups)...)
	var sets []dnsObject
	_ = json.Unmarshal(route["rule_set"], &sets)
	changed := false
	needsFrozenDefault := false
	for i, set := range sets {
		if dnsString(set, "type") != "remote" {
			continue
		}
		path := fmt.Sprintf("route.rule_set[%d]", i)
		clientRaw, hasClient := set["http_client"]
		activeClient := activeDNSRaw(clientRaw)
		if activeClient {
			result.Findings = append(result.Findings, httpReferenceInObject(clientRaw, path+".http_client", root)...)
		}
		detour := dnsString(set, "download_detour")
		if raw, present := set["download_detour"]; present {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				result.Findings = append(result.Findings, httpFailure(path+".download_detour", "http_legacy_detour_invalid", "Correct the legacy outbound reference before migration."))
				continue
			}
		}
		if activeClient && detour != "" {
			result.Findings = append(result.Findings, httpFailure(path, "http_representation_conflict", "Choose either http_client or download_detour before retrying; both representations are preserved."))
			continue
		}
		if activeClient {
			continue
		}
		if !historical {
			if detour != "" {
				result.Findings = append(result.Findings, httpFailure(path+".download_detour", "http_legacy_requires_preview", "Use compatibility preview to preserve this legacy download path before saving."))
			} else {
				result.Findings = append(result.Findings, httpWarning(path+".http_client", "http_implicit_default_retained", "The implicit shared HTTP default is retained; an empty object does not select direct."))
			}
			continue
		}
		if detour == "" && hadSharedDefault {
			result.Findings = append(result.Findings, httpFailure(path+".http_client", "http_default_intent_manual", "Resolve the old implicit download path versus the supplied shared HTTP default explicitly."))
			continue
		}
		if detour == "" {
			detour = defaultOutboundTag(root)
		}
		client, finding := httpPolicyForOutbound(root, detour, path)
		if finding != nil {
			result.Findings = append(result.Findings, *finding)
			continue
		}
		tag, updated, err := ensureHTTPPolicy(clients, client)
		if err != nil {
			result.Findings = append(result.Findings, httpFailure(path+".http_client", "http_client_identity_conflict", "A generated policy identity conflicts with an existing shared definition; rename or reconcile it explicitly."))
			continue
		}
		clients = updated
		set["http_client"] = dnsJSON(tag)
		delete(set, "download_detour")
		changed = true
		needsFrozenDefault = true
		message := "The old effective download outbound was frozen in an explicit shared HTTP client; URL and update policy are unchanged."
		if hasClient {
			message = "The empty client used the old implicit path; that path is now frozen explicitly without treating it as direct."
		}
		result.Findings = append(result.Findings, httpWarning(path+".http_client", "http_download_path_frozen", message))
	}
	// Adding the first definition also changes the manager's implicit default.
	// Freeze that separate fact before publishing any derived collection, so an
	// explicit direct rule-set cannot accidentally change another consumer.
	if needsFrozenDefault && defaultClient == "" && !hadSharedDefault {
		client, finding := httpPolicyForOutbound(root, defaultOutboundTag(root), "route.default_http_client")
		if finding != nil {
			result.Findings = append(result.Findings, *finding)
		} else {
			tag, updated, err := ensureHTTPPolicy(clients, client)
			if err != nil {
				result.Findings = append(result.Findings, httpFailure("route.default_http_client", "http_client_identity_conflict", "Reconcile the shared default identity before migration."))
			} else {
				clients = updated
				route["default_http_client"] = dnsJSON(tag)
			}
		}
	}
	if changed {
		route["rule_set"] = dnsJSON(sets)
		root["route"] = dnsJSON(route)
		root["http_clients"] = dnsJSON(clients)
	}
	return finishHTTP(result, root, changed)
}

func httpPolicyForOutbound(root dnsObject, tag, path string) (dnsObject, *diagnostics.Finding) {
	outbound, found := findDNSOutbound(root, tag)
	if tag == "" || !found {
		finding := httpFailure(path, "http_outbound_policy_manual", "Select an existing old effective outbound; its default identity cannot be proven.")
		return nil, &finding
	}
	client := dnsObject{}
	_ = json.Unmarshal(ExplicitDirectHTTPClient(), &client)
	emptyDirect := false
	if dnsString(outbound, "type") == "direct" {
		var err error
		emptyDirect, err = emptyHTTPDirectOutbound(outbound)
		if err != nil {
			finding := httpFailure(path, "http_outbound_policy_manual", "Correct the old direct outbound fields before its download policy can be frozen.")
			return nil, &finding
		}
	}
	if !emptyDirect {
		client["detour"] = dnsJSON(tag)
	}
	return client, nil
}

// ExplicitDirectHTTPClient is nonempty under complete pinned decoding. An
// empty object is not an explicit direct policy. Template factories consume
// this same fact only where their own direct outbound identity is guaranteed.
func ExplicitDirectHTTPClient() json.RawMessage {
	return json.RawMessage(`{"engine":"go","version":2}`)
}

func HTTPUnavailableEngines() map[string]string {
	return map[string]string{"apple": "http_engine_unavailable"}
}
func HTTPUnavailableVersions() map[string]string {
	if !C.WithQUIC {
		return map[string]string{"3": "http_quic_unavailable"}
	}
	return map[string]string{}
}

func ensureHTTPPolicy(clients []dnsObject, policy dnsObject) (string, []dnsObject, error) {
	var expected any
	_ = json.Unmarshal(dnsJSON(policy), &expected)
	for _, client := range clients {
		copy := dnsObject{}
		for key, value := range client {
			if key != "tag" {
				copy[key] = value
			}
		}
		var actual any
		_ = json.Unmarshal(dnsJSON(copy), &actual)
		if reflect.DeepEqual(expected, actual) {
			return dnsString(client, "tag"), clients, nil
		}
	}
	digest := sha256.Sum256(dnsJSON(policy))
	tag := fmt.Sprintf("solovey-compat-http-%x", digest[:8])
	if httpTagPresent(clients, tag) {
		return "", clients, fmt.Errorf("HTTP policy identity conflict")
	}
	policy["tag"] = dnsJSON(tag)
	return tag, append(clients, policy), nil
}

func httpTagPresent(clients []dnsObject, tag string) bool {
	for _, client := range clients {
		if dnsString(client, "tag") == tag {
			return true
		}
	}
	return false
}

// HTTPClientReferenceFindings is the sole shared-reference contract used by
// remote rule sets and TLS/provider/entity adapters. It never copies secrets.
func HTTPClientReferenceFindings(raw json.RawMessage, path string, config json.RawMessage) []diagnostics.Finding {
	var root dnsObject
	if json.Unmarshal(config, &root) != nil {
		return []diagnostics.Finding{httpFailure(path, "invalid_json", "Configuration must be an object.")}
	}
	return httpReferenceInObject(raw, path, root)
}

func httpReferenceInObject(raw json.RawMessage, path string, root dnsObject) []diagnostics.Finding {
	if !activeDNSRaw(raw) {
		return nil
	}
	var tag string
	if json.Unmarshal(raw, &tag) == nil {
		var clients []dnsObject
		_ = json.Unmarshal(root["http_clients"], &clients)
		if !httpTagPresent(clients, tag) {
			return []diagnostics.Finding{httpFailure(path, "http_client_reference_missing", "Select an existing shared HTTP client.")}
		}
		return nil
	}
	var client dnsObject
	if json.Unmarshal(raw, &client) != nil || client == nil {
		return []diagnostics.Finding{httpFailure(path, "http_client_invalid", "An HTTP client reference must be a tag or an inline object.")}
	}
	return append(httpClientShape(client, path, false), httpOutboundReference(client, path, root)...)
}

func httpOutboundReference(client dnsObject, path string, root dnsObject) []diagnostics.Finding {
	// Stored base sections have no entity catalogue. Validate outbound identity
	// against the complete candidate, whose builder always supplies these arrays.
	_, outboundsPresent := root["outbounds"]
	_, endpointsPresent := root["endpoints"]
	if !outboundsPresent && !endpointsPresent {
		return nil
	}
	if tag := dnsString(client, "detour"); tag != "" {
		outbound, found := findDNSOutbound(root, tag)
		if !found {
			return []diagnostics.Finding{httpFailure(path+".detour", "http_outbound_reference_missing", "Select an existing outbound for this HTTP client.")}
		}
		if dnsString(outbound, "type") == "direct" {
			empty, err := emptyHTTPDirectOutbound(outbound)
			if err == nil && empty {
				return []diagnostics.Finding{httpFailure(path+".detour", "http_empty_direct_detour", "Select a nonempty Go HTTP client without this empty direct detour. The pinned typed client cannot use the legacy exemption.")}
			}
		}
	}
	return nil
}

// Matches pinned direct.Outbound.IsEmpty after its injected fragment default.
// Official option decoding retains explicit false/zero distinctions correctly.
func emptyHTTPDirectOutbound(outbound dnsObject) (bool, error) {
	options := dnsObject{}
	for key, value := range outbound {
		if key != "type" && key != "tag" {
			options[key] = value
		}
	}
	var parsed option.DirectOutboundOptions
	if err := parsed.UnmarshalJSONContext(context.Background(), dnsJSON(options)); err != nil {
		return false, err
	}
	parsed.UDPFragmentDefault = true
	return reflect.DeepEqual(parsed.DialerOptions, option.DialerOptions{AbstractDialerOptions: option.AbstractDialerOptions{UDPFragmentDefault: true}}), nil
}

func httpClientShape(client dnsObject, path string, definition bool) []diagnostics.Finding {
	if client == nil {
		return []diagnostics.Finding{httpFailure(path, "http_client_invalid", "HTTP clients must be objects.")}
	}
	var findings []diagnostics.Finding
	if definition && dnsString(client, "tag") == "" {
		findings = append(findings, httpFailure(path+".tag", "http_client_tag_missing", "Shared HTTP definitions require a nonempty tag."))
	}
	if !definition {
		if _, present := client["tag"]; present {
			findings = append(findings, httpFailure(path+".tag", "http_inline_tag_invalid", "Use a string for a shared reference; an inline client cannot define a tag."))
		}
	}
	engine := dnsString(client, "engine")
	if engine != "" && engine != "go" {
		findings = append(findings, httpFailure(path+".engine", "http_engine_unavailable", "This deployment supports the Go HTTP engine; native engines have no product authorization."))
	}
	var version int
	_ = json.Unmarshal(client["version"], &version)
	if _, unavailable := HTTPUnavailableVersions()[fmt.Sprint(version)]; unavailable {
		findings = append(findings, httpFailure(path+".version", "http_quic_unavailable", "HTTP version 3 requires the declared QUIC build capability."))
	}
	var parsed option.HTTPClient
	if json.Unmarshal(dnsJSON(client), &parsed) != nil {
		findings = append(findings, httpFailure(path, "http_client_schema_invalid", "Correct the client fields for its HTTP version using the pinned schema."))
	}
	if raw, present := client["tls"]; present {
		var tls dnsObject
		_ = json.Unmarshal(raw, &tls)
		if engine := dnsString(tls, "engine"); engine != "" && engine != "go" {
			findings = append(findings, httpFailure(path+".tls.engine", "tls_engine_unavailable", "The selected native TLS engine has no product authorization."))
		}
	}
	return findings
}

func finishHTTP(result HTTPCompatibility, root dnsObject, changed bool) (HTTPCompatibility, error) {
	result.Outcome = diagnostics.Outcome(result.Findings)
	if err := diagnostics.FirstError(result.Findings); err != nil {
		return result, err
	}
	if changed {
		result.Candidate = dnsJSON(root)
	}
	return result, nil
}
func httpFailure(path, code, message string) diagnostics.Finding {
	return diagnostics.Finding{Kind: "http", Path: path, Code: code, Severity: diagnostics.Error, Message: message, MigrationOutcome: diagnostics.ManualRequired, OperatorActionRequired: true}
}
func httpWarning(path, code, message string) diagnostics.Finding {
	return diagnostics.Finding{Kind: "http", Path: path, Code: code, Severity: diagnostics.Warn, Message: message, MigrationOutcome: diagnostics.AutomaticDiagnostic, AutomaticAvailable: true}
}
