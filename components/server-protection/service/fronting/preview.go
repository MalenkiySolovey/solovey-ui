package fronting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

var (
	ErrStaleRevision = errors.New("fronting revision is stale")
	ErrUnsafeRoute   = errors.New("fronting route is unsafe")
)

type ListenSpec struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

type RouteInput struct {
	ResourceID       string     `json:"resourceId"`
	ResourceRevision string     `json:"resourceRevision"`
	SNI              []string   `json:"sni"`
	ALPN             []string   `json:"alpn"`
	Listen           ListenSpec `json:"listen"`
	ProxyProtocol    bool       `json:"proxyProtocol"`
}

type PreviewInput struct {
	ExpectedCurrentRevision string                              `json:"expectedCurrentRevision,omitempty"`
	CurrentRevision         string                              `json:"currentRevision,omitempty"`
	Resources               []hostresources.ProtectableResource `json:"-"`
	Routes                  []RouteInput                        `json:"routes"`
	FallbackResourceID      string                              `json:"fallbackResourceId,omitempty"`
}

type ManagedArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  []byte `json:"-"`
}

type FieldDiff struct {
	Field   string `json:"field"`
	Current string `json:"current"`
	Desired string `json:"desired"`
}

type Preview struct {
	DesiredRevision string            `json:"desiredRevision"`
	CurrentRevision string            `json:"currentRevision,omitempty"`
	CanonicalInput  string            `json:"canonicalInput"`
	GeneratedSHA256 string            `json:"generatedSha256"`
	GeneratedConfig string            `json:"generatedConfig"`
	ManagedPaths    []string          `json:"managedPaths"`
	Artifacts       []ManagedArtifact `json:"artifacts"`
	StaticSNI       []string          `json:"staticSni"`
	StaticALPN      []string          `json:"staticAlpn"`
	ApprovedTargets []string          `json:"approvedTargets"`
	Listens         []ListenSpec      `json:"listens"`
	Warnings        []string          `json:"warnings"`
	DisabledReasons []string          `json:"disabledReasons"`
	OutOfSync       bool              `json:"outOfSync"`
	Stale           bool              `json:"stale"`
	Fallback        string            `json:"fallback"`
	Diff            []FieldDiff       `json:"diff"`
}

type canonicalRoute struct {
	Target   string     `json:"target"`
	Revision string     `json:"revision"`
	SNI      []string   `json:"sni"`
	ALPN     []string   `json:"alpn"`
	Listen   ListenSpec `json:"listen"`
	Proxy    bool       `json:"proxy"`
}

type canonicalPreview struct {
	Fallback string           `json:"fallback"`
	Routes   []canonicalRoute `json:"routes"`
}

func (a *NginxAdapter) Preview(ctx context.Context, input PreviewInput) (Preview, error) {
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}
	return GeneratePreview(input)
}

func GeneratePreview(input PreviewInput) (Preview, error) {
	if input.ExpectedCurrentRevision != "" && input.CurrentRevision != input.ExpectedCurrentRevision {
		return Preview{}, fmt.Errorf("%w: expected %q, current %q", ErrStaleRevision, input.ExpectedCurrentRevision, input.CurrentRevision)
	}
	resources := make(map[string]hostresources.ProtectableResource, len(input.Resources))
	for _, resource := range input.Resources {
		resources[resource.ID] = resource
	}
	routes := append([]RouteInput(nil), input.Routes...)
	canonical := make([]canonicalRoute, 0, len(routes))
	seenSNI, seenALPN, proxyByTarget, proxyByListen := map[string]string{}, map[string]string{}, map[string]bool{}, map[string]bool{}
	proxyListenSeen := map[string]bool{}
	listenKeys := []ListenSpec{}
	for _, route := range routes {
		resource, ok := resources[route.ResourceID]
		if !ok {
			return Preview{}, unsafe("route target is not an approved resource")
		}
		if err := validateTarget(resource, route.ResourceRevision); err != nil {
			return Preview{}, err
		}
		listen, err := normalizeListen(route.Listen)
		if err != nil {
			return Preview{}, err
		}
		if previous, ok := proxyByTarget[resource.ID]; ok && previous != route.ProxyProtocol {
			return Preview{}, unsafe("same target has incompatible PROXY modes")
		}
		proxyByTarget[resource.ID] = route.ProxyProtocol
		if route.ProxyProtocol && resource.Capabilities.AcceptsProxyProtocol != hostresources.CapabilityYes {
			return Preview{}, unsafe("target does not confirm PROXY protocol support")
		}
		listenKey := fmt.Sprintf("%s:%d", listen.Address, listen.Port)
		if proxyListenSeen[listenKey] && proxyByListen[listenKey] != route.ProxyProtocol {
			return Preview{}, unsafe("same listener has incompatible PROXY modes")
		}
		proxyListenSeen[listenKey], proxyByListen[listenKey] = true, route.ProxyProtocol
		sni, err := normalizeSNI(route.SNI)
		if err != nil {
			return Preview{}, err
		}
		alpn, err := normalizeALPN(route.ALPN)
		if err != nil {
			return Preview{}, err
		}
		for _, key := range sni {
			if previous, exists := seenSNI[key]; exists {
				return Preview{}, unsafe("duplicate or conflicting SNI: " + key + " (" + previous + ")")
			}
			for known, target := range seenSNI {
				if wildcardOverlaps(key, known) {
					return Preview{}, unsafe("ambiguous wildcard SNI: " + key + " (" + target + ")")
				}
			}
			seenSNI[key] = resource.ID
		}
		for _, key := range alpn {
			if previous, exists := seenALPN[key]; exists {
				return Preview{}, unsafe("duplicate or conflicting ALPN: " + key + " (" + previous + ")")
			}
			seenALPN[key] = resource.ID
		}
		canonical = append(canonical, canonicalRoute{Target: resource.ID, Revision: resource.Fingerprint, SNI: sni, ALPN: alpn, Listen: listen, Proxy: route.ProxyProtocol})
		listenKeys = append(listenKeys, listen)
	}
	if err := validateListenCollisions(listenKeys); err != nil {
		return Preview{}, err
	}
	sort.Slice(canonical, func(i, j int) bool { return canonicalRouteKey(canonical[i]) < canonicalRouteKey(canonical[j]) })
	fallback, fallbackLabel, err := resolveFallback(resources, input.FallbackResourceID)
	if err != nil {
		return Preview{}, err
	}
	if fallback != nil {
		for _, enabled := range proxyByListen {
			if enabled && fallback.Capabilities.AcceptsProxyProtocol != hostresources.CapabilityYes {
				return Preview{}, unsafe("fallback does not confirm PROXY protocol support")
			}
		}
	}
	model := canonicalPreview{Fallback: fallbackLabel, Routes: canonical}
	encoded, err := json.Marshal(model)
	if err != nil {
		return Preview{}, err
	}
	desired := digest(encoded)
	config := renderNginx(model, resources, fallback)
	configSHA := digest([]byte(config))
	artifact := ManagedArtifact{Path: "nginx/stream.conf", SHA256: configSHA, Bytes: []byte(config)}
	preview := Preview{DesiredRevision: desired, CurrentRevision: input.CurrentRevision, CanonicalInput: string(encoded), GeneratedSHA256: configSHA, GeneratedConfig: config,
		ManagedPaths: []string{artifact.Path}, Artifacts: []ManagedArtifact{artifact}, StaticSNI: sortedKeys(seenSNI), StaticALPN: sortedKeys(seenALPN),
		ApprovedTargets: approvedTargets(canonical, resources), Listens: sortedListens(listenKeys), Warnings: []string{}, DisabledReasons: []string{"manual_sync_apply_requires_verified_helper_lock_acknowledgement_and_confirmation"},
		OutOfSync: input.CurrentRevision != "" && input.CurrentRevision != desired, Fallback: fallbackLabel,
		Diff: []FieldDiff{{Field: "revision", Current: input.CurrentRevision, Desired: desired}, {Field: "generated_sha256", Current: "unavailable", Desired: configSHA}}}
	if fallback == nil {
		preview.Warnings = append(preview.Warnings, "no approved local decoy: unknown, malformed TLS, and non-TLS traffic receives a boring close")
	}
	if preview.OutOfSync {
		preview.Warnings = append(preview.Warnings, "fronting preview is out of sync with the current revision")
	}
	return preview, nil
}

func validateTarget(resource hostresources.ProtectableResource, revision string) error {
	if !resource.Capabilities.Known || resource.ID == "" || resource.Owner == "" {
		return unsafe("resource owner or capabilities are incomplete")
	}
	if revision == "" || resource.Fingerprint == "" || revision != resource.Fingerprint {
		return unsafe("resource revision is stale")
	}
	if resource.Port < 1 || resource.Port > 65535 {
		return unsafe("target port is invalid")
	}
	address := net.ParseIP(strings.TrimSpace(resource.Listen))
	if address == nil || !address.IsLoopback() {
		return unsafe("target is not an approved local loopback resource")
	}
	return nil
}

func resolveFallback(resources map[string]hostresources.ProtectableResource, id string) (*hostresources.ProtectableResource, string, error) {
	if id == "" {
		return nil, "boring_close", nil
	}
	resource, ok := resources[id]
	if !ok {
		return nil, "", unsafe("fallback is not an approved resource")
	}
	if resource.Capabilities.CanServeFallback != hostresources.CapabilityYes {
		return nil, "", unsafe("fallback resource does not confirm local decoy capability")
	}
	if err := validateTarget(resource, resource.Fingerprint); err != nil {
		return nil, "", err
	}
	return &resource, resource.ID, nil
}

func normalizeListen(value ListenSpec) (ListenSpec, error) {
	address := strings.TrimSpace(value.Address)
	if value.Port < 1 || value.Port > 65535 {
		return ListenSpec{}, unsafe("listen port is invalid")
	}
	if address == "*" {
		return ListenSpec{Address: "*", Port: value.Port}, nil
	}
	parsed := net.ParseIP(address)
	if parsed == nil {
		return ListenSpec{}, unsafe("listen address is invalid")
	}
	return ListenSpec{Address: parsed.String(), Port: value.Port}, nil
}

func normalizeSNI(values []string) ([]string, error) {
	result, seen := make([]string, 0, len(values)), map[string]bool{}
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if !validSNIName(value) {
			return nil, unsafe("SNI key is invalid")
		}
		if !seen[value] {
			seen[value], result = true, append(result, value)
		}
	}
	sort.Strings(result)
	return result, nil
}

func validSNIName(value string) bool {
	if strings.HasPrefix(value, "*.") {
		value = strings.TrimPrefix(value, "*.")
	} else if strings.Contains(value, "*") {
		return false
	}
	if value == "" || len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	return true
}

func normalizeALPN(values []string) ([]string, error) {
	result, seen := make([]string, 0, len(values)), map[string]bool{}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || len(value) > 64 || strings.ContainsAny(value, " \t\r\n$;{}") {
			return nil, unsafe("ALPN key is invalid")
		}
		if !seen[value] {
			seen[value], result = true, append(result, value)
		}
	}
	sort.Strings(result)
	return result, nil
}

func validateListenCollisions(listens []ListenSpec) error {
	for i := range listens {
		for j := i + 1; j < len(listens); j++ {
			if listens[i].Port != listens[j].Port {
				continue
			}
			if listens[i].Address == listens[j].Address || listens[i].Address == "*" || listens[j].Address == "*" || dualStackWildcard(listens[i].Address, listens[j].Address) {
				return unsafe("IPv4/IPv6 wildcard or duplicate listen collision")
			}
		}
	}
	return nil
}

func dualStackWildcard(a, b string) bool {
	return (a == "0.0.0.0" && b == "::") || (a == "::" && b == "0.0.0.0")
}
func wildcardOverlaps(a, b string) bool {
	if a == b {
		return true
	}
	if strings.HasPrefix(a, "*.") {
		return strings.HasSuffix(b, strings.TrimPrefix(a, "*"))
	}
	if strings.HasPrefix(b, "*.") {
		return strings.HasSuffix(a, strings.TrimPrefix(b, "*"))
	}
	return false
}
func unsafe(reason string) error { return fmt.Errorf("%w: %s", ErrUnsafeRoute, reason) }
func digest(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func canonicalRouteKey(value canonicalRoute) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
func sortedKeys[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
func sortedListens(values []ListenSpec) []ListenSpec {
	result := append([]ListenSpec(nil), values...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Port != result[j].Port {
			return result[i].Port < result[j].Port
		}
		return result[i].Address < result[j].Address
	})
	return result
}
func approvedTargets(routes []canonicalRoute, resources map[string]hostresources.ProtectableResource) []string {
	result := make([]string, 0, len(routes))
	seen := map[string]bool{}
	for _, route := range routes {
		if !seen[route.Target] {
			resource := resources[route.Target]
			result, seen[route.Target] = append(result, resource.ID+"="+net.JoinHostPort(resource.Listen, fmt.Sprint(resource.Port))), true
		}
	}
	sort.Strings(result)
	return result
}
