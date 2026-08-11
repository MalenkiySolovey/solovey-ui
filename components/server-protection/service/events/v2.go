package events

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

func NormalizeProbeEvent(value ProbeEvent, producerEventID string) domain.ProtectionSignalV2 {
	observed := value.ObservedAt.UTC()
	if observed.IsZero() {
		observed = time.Unix(1, 0).UTC()
	}
	resourceID := strings.TrimSpace(value.ResourceID)
	resourceID = boundedLegacyID("legacy-resource", resourceID)
	subject := domain.SignalSubjectV2{Type: "endpoint", Value: resourceID}
	if prefix, err := netip.ParsePrefix(strings.TrimSpace(value.SourcePrefix)); err == nil {
		subject = domain.SignalSubjectV2{Type: "prefix", Value: prefix.Masked().String()}
	}
	category := domain.SignalCategoryEndpointObservation
	if value.SignalKind == domain.SignalResourceDrift {
		category = domain.SignalCategoryConfigDrift
	}
	kind := strings.TrimSpace(string(value.SignalKind))
	if !domain.ValidContractID(kind, 128) {
		kind = "unknown_legacy_kind"
	}
	signal := domain.ProtectionSignalV2{Schema: domain.ProtectionSignalSchemaV2, Source: domain.SignalSourceV2{SourceID: "server-protection:probe-v1", Producer: "server-protection", ProducerVersion: "v1-compat", TrustClass: "native_record_only", SourceClass: "native"}, Category: category, Kind: kind, KnownKind: domain.SignalKindKnown(category, kind), Subject: subject, Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: resourceID}, ObservedAt: observed, ExpiresAt: observed.Add(time.Hour), ConfidenceBP: compatibilityConfidence(value), SafeMeta: safeMetaV2(value.SafeMeta), Provenance: domain.SignalProvenanceV2{AdapterID: "probe-event-v1-normalizer", SourceRevision: "classifier-v" + strconv.Itoa(value.SafeMeta.ClassifierPolicyVersion), PolicyRevision: "record-only-v1", EvidenceRefIDs: []string{"probe-event:" + boundedLegacyID("legacy-event", producerEventID)}}, ReasonCodes: []string{domain.ReasonCompatibilityObserveOnly}}
	signal.FinalizeID(producerEventID)
	return signal
}

func compatibilityConfidence(value ProbeEvent) int {
	if value.SignalKind.Validate() != nil {
		return 0
	}
	return 5000
}

func safeMetaV2(value domain.SafeMeta) map[string]string {
	if value.Validate() != nil {
		return map[string]string{"classifier_policy_version": strconv.Itoa(domain.ClassifierPolicyVersion), "truncated": "true"}
	}
	result := map[string]string{}
	for key, item := range map[string]string{"path_class": value.PathClass, "ua_class": value.UAClass, "method_class": value.MethodClass, "status_class": value.StatusClass, "alpn_class": value.ALPNClass, "sni_class": value.SNIClass, "bytes_class": value.BytesClass, "duration_class": value.DurationClass} {
		if strings.TrimSpace(item) != "" {
			result[key] = item
		}
	}
	result["classifier_policy_version"] = strconv.Itoa(value.ClassifierPolicyVersion)
	if value.Truncated {
		result["truncated"] = "true"
	}
	return result
}

func ProbeEventID(id uint) string { return fmt.Sprintf("%d", id) }

func boundedLegacyID(prefix, value string) string {
	value = strings.TrimSpace(value)
	if domain.ValidContractID(value, 96) {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return prefix + ":" + hex.EncodeToString(sum[:])
}
