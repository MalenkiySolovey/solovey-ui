package firewall

import (
	"fmt"
	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"sort"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func Preview(plan FirewallPlan, options PreviewOptions) FirewallPreview {
	preview := FirewallPreview{
		Revision: plan.Revision, InputRevision: plan.InputRevision, Backend: "preview_only", WouldKeep: []string{}, WouldOpen: append([]string{}, plan.ExplicitOpen...),
		WouldWarn: append([]string{}, plan.Warnings...), WouldBlock: []string{}, Warnings: append([]string{}, plan.Warnings...),
		ProtectedKeep: append([]hostresources.ProtectableResource{}, plan.Resources...),
	}
	for _, resource := range plan.Resources {
		if resource.Port < 1 || resource.Port > 65535 {
			continue
		}
		preview.WouldKeep = append(preview.WouldKeep, fmt.Sprintf("%s %s %s:%d", resource.ID, socketProtocol(resource.Protocol), resource.Listen, resource.Port))
	}
	if planReportsUnknownSSH(plan) {
		preview.WouldWarn = append(preview.WouldWarn, "SSH is not represented in the keep policy")
	}
	sort.Strings(preview.WouldKeep)
	sort.Strings(preview.WouldOpen)
	preview.WouldWarn = uniqueSorted(preview.WouldWarn)
	preview.Warnings = uniqueSorted(preview.Warnings)
	if !options.NFTCapability.Available || strings.TrimSpace(options.NFTCapability.Revision) == "" {
		reason := strings.TrimSpace(options.NFTCapability.Reason)
		if reason == "" {
			reason = "nft_capability_unknown"
		}
		preview.Backend = "unsupported"
		preview.WouldWarn = uniqueSorted(append(preview.WouldWarn, "firewall preview script is unavailable: "+reason))
		preview.Warnings = uniqueSorted(append(preview.Warnings, "nft helper capability unavailable: inventory only"))
		return preview
	}
	if options.IncludeGeneratedNFT {
		preview.GeneratedNFT = RenderNFTPreview(plan)
	}
	return preview
}

func planReportsUnknownSSH(plan FirewallPlan) bool {
	for _, reason := range plan.BaselineEligibility.ReasonCodes {
		if reason == "ssh_management_endpoint_missing" {
			return true
		}
	}
	for _, warning := range plan.Warnings {
		if strings.Contains(strings.ToLower(warning), "ssh listener is unknown") {
			return true
		}
	}
	return false
}

func RenderNFTPreview(plan FirewallPlan) string {
	return nftables.RenderProtection(protectionIntent(plan), false, time.Time{})
}

// RenderManagedNFT is the exact apply artifact. It remains non-enforcing by
// default; a future hard-block policy must use a separate advanced-confirmed
// model rather than editing this text through the API.
func RenderManagedNFT(plan FirewallPlan) string {
	return nftables.RenderProtection(protectionIntent(plan), true, time.Time{})
}

func containsPort(values []int, port int) bool {
	index := sort.SearchInts(values, port)
	return index < len(values) && values[index] == port
}
