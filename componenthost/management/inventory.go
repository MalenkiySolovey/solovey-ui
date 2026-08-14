// Package management owns the neutral inventory and recovery-evidence boundary
// shared by the core and optional components. It intentionally contains no SSH
// mutation implementation.
package management

import (
	"context"
	"net/netip"
	"sort"
	"strings"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const inventoryTTL = 90 * time.Second

// CurrentEndpoints projects configured application intent and observed SSH
// listeners as distinct facts. Callers must never infer one from the other.
func CurrentEndpoints(ctx context.Context, now time.Time) []hostresources.ManagementEndpointV1 {
	resources := hostresources.Snapshot(ctx)
	return Endpoints(resources.Resources, hostfacts.CurrentSnapshot(), now)
}

func Endpoints(resources []hostresources.ProtectableResource, surfaces hostfacts.Snapshot, now time.Time) []hostresources.ManagementEndpointV1 {
	now = now.UTC()
	result := make([]hostresources.ManagementEndpointV1, 0)
	for _, resource := range resources {
		kind, managed := applicationKind(resource.Kind)
		if !managed {
			continue
		}
		result = append(result, configuredEndpoints(resource, kind, now)...)
	}
	for _, surface := range surfaces.Facts {
		if surface.Network != hostfacts.NetworkTCP || surface.Port == 0 || surface.Family == hostfacts.FamilyUnknown || !IsSSHSurface(surface) {
			continue
		}
		result = append(result, EndpointFromSurface(surface, now))
	}
	return dedupe(result)
}

func applicationKind(resourceKind string) (hostresources.ManagementServiceKind, bool) {
	switch strings.ToLower(strings.TrimSpace(resourceKind)) {
	case "panel_web":
		return hostresources.ManagementPanel, true
	case "subscription":
		return hostresources.ManagementSubscriptionAdmin, true
	default:
		return "", false
	}
}

func configuredEndpoints(resource hostresources.ProtectableResource, kind hostresources.ManagementServiceKind, now time.Time) []hostresources.ManagementEndpointV1 {
	keys, complete := hostresources.DeterministicConfiguredEndpointKeys(resource)
	if !complete {
		return nil
	}
	result := make([]hostresources.ManagementEndpointV1, 0, len(keys))
	for _, key := range keys {
		wildcard := hostresources.NormalizeListen(key.BindAddress).Wildcard()
		result = append(result, hostresources.ManagementEndpointV1{
			Schema:  hostresources.ManagementEndpointSchemaV1,
			ID:      "management:" + strings.TrimPrefix(resource.ID, "core:") + ":configured:" + string(key.AddressFamily),
			Network: key.Network, Family: key.AddressFamily, Bind: key.BindAddress, Port: key.Port,
			ServiceKind: kind, Exposure: hostresources.EndpointIntentForBind(key.BindAddress), Owner: resource.Owner,
			OwnerRevision: resource.Capabilities.OwnerRevision, ResourceID: resource.ID, Purpose: "administrative_access",
			RecoveryPolicy: "fresh_independent_path_required", Source: "configured-resource", ConfiguredIntent: true,
			Wildcard: wildcard, DualStack: resourceHasDualStackIntent(resource), ConfidenceBP: 10000, ObservedAt: now.Unix(), ExpiresAt: now.Add(inventoryTTL).Unix(),
			ConfigurationRevision: resource.Capabilities.ConfigRevision, SemanticRevision: resource.Capabilities.ConfigRevision,
		})
	}
	return result
}

func EndpointFromSurface(surface hostfacts.HostSurfaceFactV1, now time.Time) hostresources.ManagementEndpointV1 {
	reasons := normalizeReasons(surface.ReasonCodes)
	if surface.IsStale(now) {
		reasons = appendReason(reasons, "stale")
	}
	if surface.Truncated {
		reasons = appendReason(reasons, "truncated")
	}
	owner := strings.TrimSpace(surface.DesiredOwner)
	if owner == "" {
		owner = "unknown"
	}
	configuredRevision := surface.ConfigurationRevision
	return hostresources.ManagementEndpointV1{
		Schema: hostresources.ManagementEndpointSchemaV1, ID: "management:ssh:" + surface.ID + ":observed",
		Network: hostresources.Network(surface.Network), Family: hostresources.AddressFamily(surface.Family), Bind: surface.Bind, Port: surface.Port,
		ServiceKind: hostresources.ManagementSSH, Exposure: hostresources.EndpointIntent(surface.Exposure), Owner: owner,
		OwnerRevision: surfaceOwnerRevision(surface), ResourceID: surface.RegisteredResourceID, Purpose: "ssh_administrative_access",
		RecoveryPolicy: "fresh_independent_path_required", Source: surface.Source, ObservedListener: true,
		Wildcard: hostresources.NormalizeListen(surface.Bind).Wildcard(), DualStack: surfaceIsDualStack(surface), ConfidenceBP: surface.ConfidenceBP,
		ObservedAt: surface.LastSeen, ExpiresAt: surface.ExpiresAt, ConfigurationRevision: configuredRevision,
		RuntimeRevision: surfaceRuntimeRevision(surface), SemanticRevision: configuredRevision, ReasonCodes: reasons,
	}
}

func surfaceIsDualStack(surface hostfacts.HostSurfaceFactV1) bool {
	if surface.ListenerOwner == nil || surface.Family != hostfacts.FamilyIPv6 || !surface.ListenerOwner.Socket.Wildcard || surface.ListenerOwner.Socket.IPv6Only == nil || *surface.ListenerOwner.Socket.IPv6Only {
		return false
	}
	has4, has6 := false, false
	for _, family := range surface.ListenerOwner.Socket.CoverageFamilies {
		has4 = has4 || family == hostfacts.FamilyIPv4
		has6 = has6 || family == hostfacts.FamilyIPv6
	}
	return has4 && has6
}

func resourceHasDualStackIntent(resource hostresources.ProtectableResource) bool {
	if resource.ListenIntent.Mode == hostresources.ListenIntentDualStack {
		return true
	}
	for _, intent := range resource.ListenIntents {
		if intent.Mode == hostresources.ListenIntentDualStack {
			return true
		}
	}
	return false
}

func surfaceRuntimeRevision(surface hostfacts.HostSurfaceFactV1) string {
	if surface.ListenerOwner != nil {
		return surface.ListenerOwner.ObservationRevision
	}
	return ""
}

func surfaceOwnerRevision(surface hostfacts.HostSurfaceFactV1) string {
	if surface.ListenerOwner != nil {
		return surface.ListenerOwner.Application.ResourceOwnerRevision
	}
	return ""
}

func IsSSHSurface(surface hostfacts.HostSurfaceFactV1) bool {
	unit := strings.ToLower(strings.TrimSpace(surface.Service.SystemdUnit))
	resource := strings.ToLower(strings.TrimSpace(surface.RegisteredResourceID))
	return unit == "ssh.service" || unit == "sshd.service" || strings.HasPrefix(unit, "sshd@") && strings.HasSuffix(unit, ".service") ||
		strings.HasPrefix(resource, "core:ssh:") || resource == "core:ssh"
}

// Effective applies endpoint, family, revision, operation and expiry fences to
// persisted evidence before it reaches any mutating policy consumer.
func Effective(value hostresources.RecoveryPathV1, endpoints []hostresources.ManagementEndpointV1, now time.Time) hostresources.RecoveryPathV1 {
	if value.VerificationState != "verified" {
		return value
	}
	if value.ExpiresAt <= now.UTC().Unix() || value.ConsumedAt != 0 {
		value.VerificationState = "expired"
		value.ReasonCodes = appendReason(value.ReasonCodes, "recovery_path_expired")
		return value
	}
	matches := make([]hostresources.ManagementEndpointV1, 0, 1)
	for _, endpoint := range endpoints {
		if endpoint.ID == value.EndpointID {
			matches = append(matches, endpoint)
		}
	}
	if len(matches) != 1 || !strings.EqualFold(value.Kind, string(matches[0].ServiceKind)) || !hostresources.ManagementEndpointCurrent(matches[0], now) {
		value.VerificationState = "invalidated"
		value.ReasonCodes = appendReason(value.ReasonCodes, "management_endpoint_unavailable")
		return value
	}
	endpoint := matches[0]
	if value.ConfigurationRevision != endpoint.ConfigurationRevision {
		value.VerificationState = "invalidated"
		value.ReasonCodes = appendReason(value.ReasonCodes, "management_endpoint_revision_changed")
		return value
	}
	if value.SourcePrefix != "" {
		prefix, err := netip.ParsePrefix(value.SourcePrefix)
		familyMatches := err == nil && ((endpoint.Family == hostresources.AddressFamilyIPv4) == prefix.Addr().Is4())
		if !familyMatches {
			value.VerificationState = "invalidated"
			value.ReasonCodes = appendReason(value.ReasonCodes, "recovery_source_family_mismatch")
		}
	}
	return value
}

func dedupe(values []hostresources.ManagementEndpointV1) []hostresources.ManagementEndpointV1 {
	seen := make(map[string]struct{}, len(values))
	result := make([]hostresources.ManagementEndpointV1, 0, len(values))
	for _, value := range values {
		key := value.ID + "\x00" + string(value.Network) + "\x00" + string(value.Family) + "\x00" + value.Bind
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func normalizeReasons(values []string) []string {
	result := make([]string, 0, min(len(values), 32))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			result = appendReason(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func appendReason(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
