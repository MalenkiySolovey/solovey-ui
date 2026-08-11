package fronting

import (
	"context"
	"errors"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const SNIPrereadHealthSchemaV2 = "solovey-ui/sni-preread-health/v2"

type SNIHealthProbeV2 struct {
	ProbeID                string
	Class                  string
	ServerName             string
	AdvertisedALPN         string
	ExpectedTargetRevision string
	ExpectedUpstreamID     string
	ExpectReject           bool
}

type SNIPrereadHealthRequestV2 struct {
	OperationID              string
	OperationRevision        int
	PlanDigest               string
	CandidateRevision        string
	CandidateSHA256          string
	SocketClaimRevision      string
	SelectorSetRevision      string
	MapRevision              string
	UpstreamIDSetRevision    string
	TargetAuthorityRevisions []string
	ProxyMode                hostresources.ProxyMode
	Probes                   []SNIHealthProbeV2
}

type SNIHealthProbeEvidenceV2 struct {
	ProbeID                 string `json:"probeId"`
	ExpectedTargetRevision  string `json:"expectedTargetRevision,omitempty"`
	ObservedTargetRevision  string `json:"observedTargetRevision,omitempty"`
	BackendIdentityMarker   string `json:"backendIdentityMarker,omitempty"`
	ConnectionRejected      bool   `json:"connectionRejected"`
	ExpectedBackendReached  bool   `json:"expectedBackendReached"`
	AlternateTargetReceipts uint32 `json:"alternateTargetReceipts"`
	ProxyHeaderObserved     bool   `json:"proxyHeaderObserved"`
}

type SNIPrereadHealthEvidenceV2 struct {
	Schema                   string                     `json:"schema"`
	OperationID              string                     `json:"operationId"`
	OperationRevision        int                        `json:"operationRevision"`
	PlanDigest               string                     `json:"planDigest"`
	CandidateRevision        string                     `json:"candidateRevision"`
	CandidateSHA256          string                     `json:"candidateSha256"`
	SocketClaimRevision      string                     `json:"socketClaimRevision"`
	SelectorSetRevision      string                     `json:"selectorSetRevision"`
	MapRevision              string                     `json:"mapRevision"`
	UpstreamIDSetRevision    string                     `json:"upstreamIdSetRevision"`
	TargetAuthorityRevisions []string                   `json:"targetAuthorityRevisions"`
	ProxyMode                hostresources.ProxyMode    `json:"proxyMode"`
	Probes                   []SNIHealthProbeEvidenceV2 `json:"probes"`
	ObservedAt               int64                      `json:"observedAt"`
	ExpiresAt                int64                      `json:"expiresAt"`
	LatencyMilliseconds      uint32                     `json:"latencyMilliseconds"`
	ReasonCodes              []string                   `json:"reasonCodes,omitempty"`
}

type SNIPrereadHealthCheckV2 func(context.Context, SNIPrereadHealthRequestV2) (SNIPrereadHealthEvidenceV2, error)

func buildSNIHealthRequestV2(operationID string, operationRevision int, checkpoint CheckpointV2) (SNIPrereadHealthRequestV2, error) {
	authorities, err := authorityRevisionsV2(checkpoint)
	if err != nil {
		return SNIPrereadHealthRequestV2{}, err
	}
	authorityRevisions := make([]string, 0, len(checkpoint.Plan.Selectors.TargetRevisions))
	for _, reference := range checkpoint.Plan.Selectors.TargetRevisions {
		revision := authorities[reference]
		if !frontingHexV2(revision) {
			return SNIPrereadHealthRequestV2{}, errors.New("target_authority_stale")
		}
		authorityRevisions = append(authorityRevisions, revision)
	}
	probes := make([]SNIHealthProbeV2, 0, len(checkpoint.Plan.Selectors.Tuples)+4)
	for _, tuple := range checkpoint.Plan.Selectors.Tuples {
		class, alpn := "SNI_ONLY", ""
		if tuple.ALPN != "" {
			class, alpn = "SNI_ALPN", tuple.ALPN
		}
		probes = append(probes, SNIHealthProbeV2{ProbeID: v2Revision(struct{ Selector, Class string }{tuple.SelectorID, class}), Class: class,
			ServerName: tuple.SNI, AdvertisedALPN: alpn, ExpectedTargetRevision: tuple.TargetReferenceRevision, ExpectedUpstreamID: tuple.UpstreamID})
	}
	unknown := "solovey-unknown.invalid"
	for selectorContainsSNIV2(checkpoint.Plan.Selectors, unknown) {
		unknown = "x." + unknown
	}
	resolution := ResolveFiniteSelectorV2(checkpoint.Plan.Selectors, unknown, "h2")
	probes = append(probes, SNIHealthProbeV2{ProbeID: v2Revision("unknown-sni"), Class: "UNKNOWN_SNI", ServerName: unknown, AdvertisedALPN: "h2",
		ExpectedTargetRevision: resolution.TargetReferenceRevision, ExpectedUpstreamID: resolution.UpstreamID, ExpectReject: resolution.Rejected})
	for _, class := range []string{"MISSING_SNI", "MALFORMED_TLS", "NON_TLS"} {
		probes = append(probes, SNIHealthProbeV2{ProbeID: v2Revision(class), Class: class, ExpectReject: true})
	}
	return SNIPrereadHealthRequestV2{OperationID: operationID, OperationRevision: operationRevision, PlanDigest: checkpoint.Plan.CanonicalPlanDigest,
		CandidateRevision: checkpoint.CandidateRevision, CandidateSHA256: checkpoint.CandidateSHA256, SocketClaimRevision: checkpoint.SocketClaimRevision,
		SelectorSetRevision: checkpoint.SelectorSetRevision, MapRevision: checkpoint.MapRevision, UpstreamIDSetRevision: checkpoint.UpstreamIDSetRevision,
		TargetAuthorityRevisions: authorityRevisions, ProxyMode: checkpoint.SelectedProxyMode, Probes: probes}, nil
}

func selectorContainsSNIV2(set SelectorSetV1, sni string) bool {
	for _, tuple := range set.Tuples {
		if tuple.SNI == sni {
			return true
		}
	}
	return false
}

func validateSNIHealthEvidenceV2(request SNIPrereadHealthRequestV2, evidence SNIPrereadHealthEvidenceV2, now time.Time) error {
	if evidence.Schema != SNIPrereadHealthSchemaV2 || evidence.OperationID != request.OperationID || evidence.OperationRevision != request.OperationRevision ||
		evidence.PlanDigest != request.PlanDigest || evidence.CandidateRevision != request.CandidateRevision || evidence.CandidateSHA256 != request.CandidateSHA256 ||
		evidence.SocketClaimRevision != request.SocketClaimRevision || evidence.SelectorSetRevision != request.SelectorSetRevision || evidence.MapRevision != request.MapRevision ||
		evidence.UpstreamIDSetRevision != request.UpstreamIDSetRevision || evidence.ProxyMode != request.ProxyMode ||
		!equalStringsV2(evidence.TargetAuthorityRevisions, request.TargetAuthorityRevisions) || len(evidence.Probes) != len(request.Probes) ||
		evidence.ObservedAt <= 0 || evidence.ExpiresAt <= evidence.ObservedAt || evidence.ExpiresAt-evidence.ObservedAt > int64(frontingHealthTimeout/time.Second) ||
		evidence.ObservedAt > now.Unix() || evidence.ExpiresAt <= now.Unix() || evidence.LatencyMilliseconds > uint32(frontingHealthTimeout/time.Millisecond) || len(evidence.ReasonCodes) != 0 {
		return errors.New("health_failed")
	}
	wantProxy := request.ProxyMode == hostresources.ProxyModeOn
	for index, probe := range request.Probes {
		observed := evidence.Probes[index]
		if observed.ProbeID != probe.ProbeID || observed.ExpectedTargetRevision != probe.ExpectedTargetRevision || observed.AlternateTargetReceipts != 0 {
			return errors.New("health_failed")
		}
		if probe.ExpectReject {
			if !observed.ConnectionRejected || observed.ExpectedBackendReached || observed.ObservedTargetRevision != "" || observed.BackendIdentityMarker != "" || observed.ProxyHeaderObserved {
				return errors.New("health_failed")
			}
			continue
		}
		if observed.ConnectionRejected || !observed.ExpectedBackendReached || observed.ObservedTargetRevision != probe.ExpectedTargetRevision ||
			observed.BackendIdentityMarker != probe.ExpectedTargetRevision || observed.ProxyHeaderObserved != wantProxy {
			return errors.New("health_failed")
		}
	}
	return nil
}

func equalStringsV2(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type sniHealthResultV2 struct {
	evidence SNIPrereadHealthEvidenceV2
	err      error
}

func boundedSNIHealthV2(ctx context.Context, check SNIPrereadHealthCheckV2, request SNIPrereadHealthRequestV2) (SNIPrereadHealthEvidenceV2, error) {
	if check == nil {
		return SNIPrereadHealthEvidenceV2{}, errors.New("health_failed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	healthCtx, cancel := context.WithTimeout(ctx, frontingHealthTimeout)
	defer cancel()
	result := make(chan sniHealthResultV2, 1)
	go func() {
		defer func() {
			if recover() != nil {
				result <- sniHealthResultV2{err: errors.New("health_failed")}
			}
		}()
		evidence, err := check(healthCtx, request)
		result <- sniHealthResultV2{evidence: evidence, err: err}
	}()
	select {
	case value := <-result:
		return value.evidence, value.err
	case <-healthCtx.Done():
		return SNIPrereadHealthEvidenceV2{}, errors.New("health_failed")
	}
}
