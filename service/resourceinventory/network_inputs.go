package resourceinventory

import (
	"encoding/hex"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	identity "github.com/MalenkiySolovey/solovey-ui/internal/networkidentity"
)

func configuredNetworkEvidence(source, revision string, observedAt int64) identity.Evidence {
	return identity.Evidence{Availability: identity.Available, Source: "resource-inventory:" + source, Authority: "CONFIGURED_INPUT",
		ObservedAt: observedAt, ExpiresAt: observedAt + 5, Revision: revision, Reason: "configuration_input_not_runtime_or_reachability_proof"}
}

// Project only existing owner configuration. Do not resolve names, infer an
// advertised winner, use browser Host, or turn a socket bind into a WAN fact.
func projectConfiguredNetworkInputs(result *identity.Snapshot, resources hostresources.ResourceSnapshot, now time.Time) {
	if resources.GeneratedAt <= 0 || resources.GeneratedAt > now.Unix() || resources.GeneratedAt+5 <= now.Unix() ||
		len(resources.Resources) == 0 || len(resources.Resources) > hostresources.MaxResourceFacts {
		return
	}
	complete := len(resources.Errors) == 0
	for _, resource := range resources.Resources {
		if !safeNetworkInputText(resource.ID, 160) || !safeNetworkInputText(resource.Owner, 64) ||
			!safeNetworkInputText(resource.Source, 128) || resource.Source == "unknown" || !networkDigest(resource.Capabilities.ConfigRevision) {
			complete = false
			continue
		}
		evidence := configuredNetworkEvidence(resource.Source, resource.Capabilities.ConfigRevision, resources.GeneratedAt)
		intents, valid := hostresources.ConfiguredListenIntents(resource)
		if valid {
			for _, intent := range intents {
				if len(result.Listen.Values) >= hostresources.MaxResourceFacts {
					complete = false
					break
				}
				result.Listen.Values = append(result.Listen.Values, identity.EndpointInput{Kind: "listen-intent", ResourceID: resource.ID,
					Owner: resource.Owner, Address: intent.Address, Port: intent.Port, Network: string(intent.Network),
					ConfigurationRevision: intent.ConfigurationRevision, Evidence: evidence})
			}
		} else {
			complete = false
		}
		if len(resource.AdvertisedEndpoints) > hostresources.MaxAdvertisedEndpointsPerResource || len(resource.Capabilities.PublicHostnames) > 32 {
			complete = false
			continue
		}
		appendAdvertised := func(value identity.EndpointInput) {
			if len(result.Advertised.Values) >= hostresources.MaxResourceFacts || !safeNetworkInputText(value.Address, 256) ||
				value.Port == 0 || (value.Network != string(hostresources.NetworkTCP) && value.Network != string(hostresources.NetworkUDP)) {
				complete = false
				return
			}
			result.Advertised.Values = append(result.Advertised.Values, value)
		}
		for _, endpoint := range resource.AdvertisedEndpoints {
			appendAdvertised(identity.EndpointInput{Kind: "advertised-endpoint", EndpointID: endpoint.ID, ResourceID: resource.ID,
				Owner: resource.Owner, Address: endpoint.HostnameOrIP, Port: endpoint.Port, Network: string(endpoint.Network),
				ConfigurationRevision: resource.Capabilities.ConfigRevision, Evidence: evidence})
		}
		for _, hostname := range resource.Capabilities.PublicHostnames {
			if resource.Port <= 0 || resource.Port > 65535 {
				complete = false
				continue
			}
			appendAdvertised(identity.EndpointInput{Kind: "configured-hostname", ResourceID: resource.ID, Owner: resource.Owner,
				Address: hostname, Port: uint16(resource.Port), Network: string(hostresources.NetworkForProtocol(resource.Protocol)),
				ConfigurationRevision: resource.Capabilities.ConfigRevision, Evidence: evidence})
		}
	}
	for _, facts := range []*identity.EndpointFacts{&result.Listen, &result.Advertised} {
		sort.Slice(facts.Values, func(i, j int) bool {
			a, b := facts.Values[i], facts.Values[j]
			if a.ResourceID != b.ResourceID {
				return a.ResourceID < b.ResourceID
			}
			if a.Address != b.Address {
				return a.Address < b.Address
			}
			if a.Network != b.Network {
				return a.Network < b.Network
			}
			if a.Port != b.Port {
				return a.Port < b.Port
			}
			return a.Kind+":"+a.EndpointID < b.Kind+":"+b.EndpointID
		})
		facts.Evidence = configuredNetworkEvidence("configured-inputs", identity.Revision(facts.Values), resources.GeneratedAt)
		if !complete {
			facts.Evidence.Availability, facts.Evidence.Reason = identity.Unknown, "configured_resource_inventory_incomplete"
		}
	}
}

func safeNetworkInputText(value string, limit int) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || len(value) > limit {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func networkDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
