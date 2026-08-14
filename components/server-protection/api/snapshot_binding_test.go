//go:build !minimal

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func TestFirewallBaselineCapabilityAssessmentKeepsUnprovenAdvancedPrimitivesHonest(t *testing.T) {
	capabilities := &protectionhelper.CapabilitiesResult{Revision: strings.Repeat("a", 64), NFT: protectionhelper.NFTSupport{PlatformKnown: true, Linux: true, Available: true}, Capabilities: []protectionhelper.Capability{{Operation: protectionhelper.OperationNFTValidate, Available: true}}}
	baseline := protectionfirewall.AssessBaselineCapabilities(protectionfirewall.FirewallPlan{}, capabilities)
	if !baseline.CandidateSupported || baseline.TTLSupported || baseline.RateSupported || baseline.AdvancedState != "DEFERRED_UNPROVEN" || baseline.Consequence != "BASELINE_ONLY_ADVANCED_SCENARIOS_NOT_SHIPPED" {
		t.Fatalf("unproven advanced capabilities were overstated or blocked the primitive-free baseline: %#v", baseline)
	}
	timed := protectionfirewall.FirewallPlan{Endpoints: []protectionfirewall.EndpointPolicy{{Contributions: []protectionfirewall.EndpointContribution{{Intent: domain.IntentTemporaryBlock}}}}}
	blocked := protectionfirewall.AssessBaselineCapabilities(timed, capabilities)
	if blocked.CandidateSupported || !blocked.TTLRequired || blocked.Consequence != "BASELINE_BLOCKED" {
		t.Fatalf("mandatory unsupported TTL primitive did not block candidate: %#v", blocked)
	}
	capabilities.NFT.TTLSet, capabilities.NFT.RateLimit = true, true
	rate := protectionfirewall.FirewallPlan{Endpoints: []protectionfirewall.EndpointPolicy{{Contributions: []protectionfirewall.EndpointContribution{{Intent: domain.IntentRateLimit}}}}}
	supported := protectionfirewall.AssessBaselineCapabilities(rate, capabilities)
	if !supported.CandidateSupported || !supported.TTLRequired || !supported.RateRequired || supported.AdvancedState != "SUPPORTED_BY_READ_ONLY_CHECK" {
		t.Fatalf("proven mandatory primitives were not accepted: %#v", supported)
	}
}

func TestFirewallBaselineSingleSnapshotBindsPlanPreviewAndCanonicalResources(t *testing.T) {
	router, _ := newProtectionAPIRouter(t, writeScope)
	read := func() struct {
		SnapshotBinding firewallBaselineSnapshotBinding `json:"snapshotBinding"`
		SocketGraph     struct {
			Revision string `json:"revision"`
		} `json:"socketGraph"`
		SocketGraphEvidence struct {
			Revision      string `json:"revision"`
			GraphRevision string `json:"graphRevision"`
			Nodes         []struct {
				ResourceID   string   `json:"resourceId"`
				ApplyBlocked bool     `json:"applyBlocked"`
				ReasonCodes  []string `json:"reasonCodes"`
			} `json:"nodes"`
		} `json:"socketGraphEvidence"`
		KernelPlan struct {
			Revision      string `json:"revision"`
			InputRevision string `json:"inputRevision"`
		} `json:"kernelPlan"`
		KernelPreview struct {
			Revision      string `json:"revision"`
			InputRevision string `json:"inputRevision"`
		} `json:"kernelPreview"`
	} {
		response := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/firewall-baseline?refresh=true&include_generated_nft=true", "")
		var value struct {
			SnapshotBinding firewallBaselineSnapshotBinding `json:"snapshotBinding"`
			SocketGraph     struct {
				Revision string `json:"revision"`
			} `json:"socketGraph"`
			SocketGraphEvidence struct {
				Revision      string `json:"revision"`
				GraphRevision string `json:"graphRevision"`
				Nodes         []struct {
					ResourceID   string   `json:"resourceId"`
					ApplyBlocked bool     `json:"applyBlocked"`
					ReasonCodes  []string `json:"reasonCodes"`
				} `json:"nodes"`
			} `json:"socketGraphEvidence"`
			KernelPlan struct {
				Revision      string `json:"revision"`
				InputRevision string `json:"inputRevision"`
			} `json:"kernelPlan"`
			KernelPreview struct {
				Revision      string `json:"revision"`
				InputRevision string `json:"inputRevision"`
			} `json:"kernelPreview"`
		}
		decodeProtectionObject(t, response, &value)
		return value
	}
	first, second := read(), read()
	for _, value := range []struct {
		SnapshotBinding firewallBaselineSnapshotBinding
		Graph           string
		GraphEvidence   string
		EvidenceGraph   string
		EvidenceNodes   int
		Plan            string
		Input           string
		PreviewPlan     string
		PreviewInput    string
	}{{first.SnapshotBinding, first.SocketGraph.Revision, first.SocketGraphEvidence.Revision, first.SocketGraphEvidence.GraphRevision, len(first.SocketGraphEvidence.Nodes), first.KernelPlan.Revision, first.KernelPlan.InputRevision, first.KernelPreview.Revision, first.KernelPreview.InputRevision}, {second.SnapshotBinding, second.SocketGraph.Revision, second.SocketGraphEvidence.Revision, second.SocketGraphEvidence.GraphRevision, len(second.SocketGraphEvidence.Nodes), second.KernelPlan.Revision, second.KernelPlan.InputRevision, second.KernelPreview.Revision, second.KernelPreview.InputRevision}} {
		if value.SnapshotBinding.Schema != firewallBaselineSnapshotBindingSchemaV1 || value.SnapshotBinding.Revision != value.Input || value.SnapshotBinding.GraphRevision != value.Graph || value.SnapshotBinding.GraphEvidenceRevision != value.GraphEvidence || value.EvidenceGraph != value.Graph || value.EvidenceNodes == 0 || value.SnapshotBinding.PlanRevision != value.Plan || value.PreviewPlan != value.Plan || value.PreviewInput != value.Input || !exactAPIRevision(value.SnapshotBinding.CandidateSHA256) {
			t.Fatalf("cross-snapshot firewall baseline response: %#v", value)
		}
	}
	if first.SnapshotBinding.Revision != second.SnapshotBinding.Revision || first.KernelPlan.Revision != second.KernelPlan.Revision {
		t.Fatal("same semantic input changed revision across refreshed observation timestamps")
	}
}

func TestFirewallPreviewOptimisticBindingRejectsStaleAndMalformedSnapshots(t *testing.T) {
	router, _ := newProtectionAPIRouter(t, writeScope)
	baselineResponse := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/firewall-baseline?refresh=true", "")
	var baseline struct {
		SnapshotBinding firewallBaselineSnapshotBinding `json:"snapshotBinding"`
	}
	decodeProtectionObject(t, baselineResponse, &baseline)
	validBody, _ := json.Marshal(map[string]any{"includeGeneratedNft": true, "expectedBindingRevision": baseline.SnapshotBinding.Revision})
	valid := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/firewall/preview", string(validBody))
	if valid.Code != http.StatusOK {
		t.Fatalf("current binding was rejected: %d %s", valid.Code, valid.Body.String())
	}
	for _, revision := range []string{strings.Repeat("f", 64), "malformed"} {
		body, _ := json.Marshal(map[string]any{"expectedBindingRevision": revision})
		response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/firewall/preview", string(body))
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "revision_conflict") {
			t.Fatalf("stale/malformed binding did not fail closed: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestDirectDatabaseRecoveryFixtureCannotSatisfyProductionVerification(t *testing.T) {
	_, _, _, repository, db := newProtectionAPIRouterWithDB(t, readScope, protectionfronting.NewNginxAdapter())
	now := time.Now().UTC().Truncate(time.Second)
	resource := hostresources.ProtectableResource{ID: "core:panel:web", Kind: "panel_web", Owner: "panel", Protocol: "tcp", Listen: "192.0.2.5", Port: 443, Public: true, Source: "fixture", Fingerprint: strings.Repeat("d", 64), Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: strings.Repeat("a", 64), ConfigRevision: strings.Repeat("c", 64)}}
	endpoint := hostresources.ManagementEndpointFromResource(resource, hostresources.ManagementPanel, now)
	row := protectionrepository.RecoveryPathModel{RecoveryPathID: "recovery:direct", Kind: string(hostresources.ManagementPanel), EndpointID: endpoint.ID, PrincipalID: "principal:direct", SourcePrefix: "198.51.100.10/32", VerificationMethod: "fresh_panel_login", VerifiedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), IndependenceClass: "independent_reconnect", VerificationState: "verified", ReasonCodesJSON: []byte(`[]`), SourceRevision: strings.Repeat("b", 64), ConfigurationRevision: endpoint.ConfigurationRevision}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	handler := Handler{deps: Deps{Repository: repository}}
	_, paths, invalid, err := handler.managementContracts(context.Background(), []hostresources.ProtectableResource{resource}, hostfacts.Snapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 || invalid != 0 {
		t.Fatalf("unsealed direct database row became production recovery proof: paths=%d invalid=%d", len(paths), invalid)
	}
}
