package nativefallback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"sort"
	"strings"
	"time"

	managementregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const managementFactLifetime = time.Minute

type InventoryManagementReader struct {
	Now       func() time.Time
	Endpoints func(context.Context, time.Time) []hostresources.ManagementEndpointV1
	Resources func(context.Context) hostresources.ResourceSnapshot
}

func (reader InventoryManagementReader) ResolveIsolation(ctx context.Context, resourceID string, target ManagementEndpointFactsV1) (ManagementIsolationResultV1, error) {
	if err := ctx.Err(); err != nil {
		return ManagementIsolationResultV1{}, err
	}
	now := time.Now().UTC()
	if reader.Now != nil {
		now = reader.Now().UTC()
	}
	now = now.Truncate(time.Second)
	result := ManagementIsolationResultV1{State: "ISOLATED", ExpiresAt: now.Add(managementFactLifetime)}
	if strings.EqualFold(target.ManagementReachability, string(hostresources.CapabilityYes)) {
		result.State = "FORBIDDEN"
		result.ReasonCodes = append(result.ReasonCodes, "management_target_forbidden")
	} else if !strings.EqualFold(target.ManagementReachability, string(hostresources.CapabilityNo)) {
		result.State = "UNKNOWN"
		result.ReasonCodes = append(result.ReasonCodes, "management_reachability_unknown")
	}
	address, addressErr := netip.ParseAddr(target.Address)
	if addressErr != nil || !target.Local || !address.IsLoopback() {
		result.State = "FORBIDDEN"
		result.ReasonCodes = append(result.ReasonCodes, "management_target_forbidden")
	}
	endpoints := managementregistry.CurrentEndpoints
	if reader.Endpoints != nil {
		endpoints = reader.Endpoints
	}
	management := endpoints(ctx, now)
	if err := ctx.Err(); err != nil {
		return ManagementIsolationResultV1{}, err
	}
	resources := hostresources.Snapshot(ctx)
	if reader.Resources != nil {
		resources = reader.Resources(ctx)
	}
	if len(resources.Errors) != 0 {
		result.State = "UNKNOWN"
		result.ReasonCodes = append(result.ReasonCodes, "management_reachability_unknown")
	}
	for _, endpoint := range management {
		if !hostresources.ManagementEndpointCurrent(endpoint) {
			result.State = "UNKNOWN"
			result.ReasonCodes = append(result.ReasonCodes, "management_reachability_unknown")
			continue
		}
		if managementEndpointOverlaps(target, endpoint.Network, endpoint.Family, endpoint.Bind, endpoint.Port) {
			result.State = "FORBIDDEN"
			result.ReasonCodes = append(result.ReasonCodes, "management_target_forbidden")
		}
	}
	for _, resource := range resources.Resources {
		kind := strings.ToLower(strings.TrimSpace(resource.Kind))
		if kind != "node_control" && kind != "panel_web" && kind != "subscription" {
			continue
		}
		keys, complete := hostresources.DeterministicConfiguredEndpointKeys(resource)
		if !complete {
			result.State = "UNKNOWN"
			result.ReasonCodes = append(result.ReasonCodes, "management_reachability_unknown")
			continue
		}
		for _, key := range keys {
			if managementEndpointOverlaps(target, key.Network, key.AddressFamily, key.BindAddress, key.Port) {
				result.State = "FORBIDDEN"
				result.ReasonCodes = append(result.ReasonCodes, "management_target_forbidden")
			}
		}
	}
	result.ReasonCodes = canonicalStrings(result.ReasonCodes)
	result.Revision = managementRevision(resourceID, target, management, resources)
	return result, nil
}

func managementEndpointOverlaps(target ManagementEndpointFactsV1, network hostresources.Network, family hostresources.AddressFamily, bind string, port uint16) bool {
	if network != hostresources.NetworkTCP || string(family) != target.AddressFamily || port == 0 || port != target.Port {
		return false
	}
	normalized := hostresources.NormalizeListen(bind)
	if normalized.Wildcard() {
		return true
	}
	left, leftErr := netip.ParseAddr(normalized.Value)
	right, rightErr := netip.ParseAddr(target.Address)
	return leftErr == nil && rightErr == nil && left.Unmap() == right.Unmap()
}

func managementRevision(resourceID string, target ManagementEndpointFactsV1, endpoints []hostresources.ManagementEndpointV1, resources hostresources.ResourceSnapshot) string {
	endpoints = append([]hostresources.ManagementEndpointV1(nil), endpoints...)
	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].ID < endpoints[j].ID })
	resourceFacts := make([]struct{ ID, Kind, Revision string }, 0)
	for _, resource := range resources.Resources {
		kind := strings.ToLower(strings.TrimSpace(resource.Kind))
		if kind == "node_control" || kind == "panel_web" || kind == "subscription" {
			resourceFacts = append(resourceFacts, struct{ ID, Kind, Revision string }{resource.ID, kind, resource.Capabilities.ConfigRevision})
		}
	}
	sort.Slice(resourceFacts, func(i, j int) bool { return resourceFacts[i].ID < resourceFacts[j].ID })
	payload, _ := json.Marshal(struct {
		Schema     string
		ResourceID string
		Target     ManagementEndpointFactsV1
		Endpoints  []hostresources.ManagementEndpointV1
		Resources  []struct{ ID, Kind, Revision string }
	}{"solovey-ui/native-fallback-management-isolation/v1", resourceID, target, endpoints, resourceFacts})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func canonicalStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
