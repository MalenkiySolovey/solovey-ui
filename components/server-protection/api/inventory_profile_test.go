//go:build !minimal

package api

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

func TestMVPAPIInventoryProfileAndMissingApplyCapability(t *testing.T) {
	router, audits := newProtectionAPIRouter(t, "admin")

	status := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/status", "")
	statusEnvelope := assertProtectionSuccess(t, status)
	var statusSchema map[string]json.RawMessage
	if err := json.Unmarshal(statusEnvelope.Obj, &statusSchema); err != nil {
		t.Fatalf("decode status schema: %v", err)
	}
	for _, field := range []string{"enabled", "revision", "supportState", "readiness", "blockers", "counters"} {
		if _, ok := statusSchema[field]; !ok {
			t.Fatalf("status response lost stable field %q: %s", field, statusEnvelope.Obj)
		}
	}
	resources := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/resources?refresh=true", "")
	var inventory struct {
		Resources []hostresources.ProtectableResource `json:"resources"`
	}
	decodeProtectionObject(t, resources, &inventory)
	if len(inventory.Resources) != 1 || inventory.Resources[0].ID != "fixture:listener:one" {
		t.Fatalf("inventory = %#v", inventory)
	}

	create := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/profiles", `{"resourceId":"fixture:listener:one","resourceRevision":"`+inventory.Resources[0].Fingerprint+`","mode":"record_only","enabled":true,"defaultAction":"record_only"}`)
	var profile profileView
	decodeProtectionObject(t, create, &profile)
	if profile.ResourceID != "fixture:listener:one" || profile.Mode != domain.ProfileModeRecordOnly || profile.Revision != 1 {
		t.Fatalf("created profile = %#v", profile)
	}

	update := requestProtectionAPI(router, http.MethodPut, "/api/components/server-protection/profiles/1", `{"mode":"record_only","enabled":false,"scoreThreshold":5,"graylistTtlSeconds":3600,"defaultAction":"record_only","revision":1}`)
	decodeProtectionObject(t, update, &profile)
	if profile.Enabled || profile.Revision != 2 || profile.Status != "disabled" {
		t.Fatalf("updated profile = %#v", profile)
	}
	stale := requestProtectionAPI(router, http.MethodPut, "/api/components/server-protection/profiles/1", `{"mode":"record_only","enabled":true,"scoreThreshold":5,"graylistTtlSeconds":3600,"defaultAction":"record_only","revision":1}`)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "revision_conflict") {
		t.Fatalf("stale profile update = %d %s", stale.Code, stale.Body.String())
	}
	previewResponse := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/firewall/preview", `{"includeGeneratedNft":true}`)
	var preview struct {
		Backend      string   `json:"backend"`
		WouldBlock   []string `json:"wouldBlock"`
		GeneratedNFT string   `json:"generatedNft"`
	}
	decodeProtectionObject(t, previewResponse, &preview)
	wantBackend, wantGeneratedNFT := "unsupported", false
	if runtime.GOOS == "linux" {
		wantBackend, wantGeneratedNFT = "preview_only", true
	}
	if preview.Backend != wantBackend || len(preview.WouldBlock) != 0 || (wantGeneratedNFT != (preview.GeneratedNFT != "")) {
		t.Fatalf("preview = %#v, want backend=%q generated=%t", preview, wantBackend, wantGeneratedNFT)
	}

	operationID := "unavailable-firewall-operation"
	apply := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/firewall/apply", `{"operationId":"`+operationID+`","confirmation":"APPLY SERVER PROTECTION `+operationID+`"}`)
	if apply.Code != http.StatusConflict || !strings.Contains(apply.Body.String(), "missing_capability") {
		t.Fatalf("apply response = %d %s", apply.Code, apply.Body.String())
	}
	if len(*audits) != 4 {
		t.Fatalf("audit events = %#v", *audits)
	}
}

func TestMetadataOnlyProfileNeedsNoMutationCapability(t *testing.T) {
	router, _ := newProtectionAPIRouter(t, "admin") // The fixture deliberately has no firewall workflow.
	resources := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/resources?refresh=true", "")
	var inventory struct {
		Resources []hostresources.ProtectableResource `json:"resources"`
	}
	decodeProtectionObject(t, resources, &inventory)
	if len(inventory.Resources) != 1 {
		t.Fatalf("resources = %#v", inventory)
	}

	created := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/profiles", `{"resourceId":"fixture:listener:one","resourceRevision":"`+inventory.Resources[0].Fingerprint+`","mode":"metadata_only","enabled":true,"defaultAction":"record_only"}`)
	var profile profileView
	decodeProtectionObject(t, created, &profile)
	if profile.Mode != domain.ProfileModeMetadataOnly || !profile.Enabled {
		t.Fatalf("metadata profile = %#v", profile)
	}
}
