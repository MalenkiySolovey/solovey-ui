package coreinboundcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"strings"
	"time"

	coreregistry "github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	sb "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
	"gorm.io/gorm"
)

const (
	previewLifetime = 5 * time.Minute
)

type defaultCandidateValidator struct{}

func (defaultCandidateValidator) ValidateInbound(ctx context.Context, content []byte) error {
	if _, err := canonicalInboundOptionsDigest(ctx, content); err != nil {
		return err
	}
	complete, err := json.Marshal(struct {
		Inbounds []json.RawMessage `json:"inbounds"`
	}{[]json.RawMessage{content}})
	if err != nil {
		return err
	}
	return singboxvalidation.ValidateConfig(complete)
}

func (s *Service) PreviewFallbackPatch(ctx context.Context, request PreviewFallbackPatchRequestV1) (FallbackPatchPreviewV1, error) {
	if err := ctx.Err(); err != nil {
		return FallbackPatchPreviewV1{}, adapterFailure(ErrorCancelled)
	}
	if s == nil || s.db == nil {
		return FallbackPatchPreviewV1{}, adapterFailure(ErrorDatabase)
	}
	endpoint, err := validateApprovedEndpoint(request.Variant, request.ApprovedEndpoint)
	if err != nil {
		return FallbackPatchPreviewV1{}, err
	}
	if request.Expected.EndpointRevision == "" || request.Expected.EndpointRevision != endpoint.EndpointRevision {
		return FallbackPatchPreviewV1{}, adapterFailure(ErrorInvalidEndpoint)
	}
	request.ApprovedEndpoint = endpoint
	var preview FallbackPatchPreviewV1
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		inbound, referenceCount, snapshot, loadErr := s.loadInboundState(tx, request.Expected.InboundDatabaseID)
		if loadErr != nil {
			return loadErr
		}
		if checkErr := s.checkPreviewExpectations(snapshot, request); checkErr != nil {
			return checkErr
		}
		candidate, changed, _, expectedOptionsDigest, candidateErr := s.candidateFor(ctx, tx, inbound, referenceCount, request)
		if candidateErr != nil {
			return candidateErr
		}
		after := buildSnapshotWithRuntimeDigest(candidate, referenceCount, s.identity, nil, 0, expectedOptionsDigest)
		now := s.now().Truncate(time.Second)
		preview = FallbackPatchPreviewV1{
			Schema: FallbackPatchPreviewSchemaV1, InboundDatabaseID: snapshot.InboundDatabaseID,
			ResourceID: snapshot.ResourceID, Variant: request.Variant,
			BeforeConfigurationRevision: snapshot.ConfigurationRevision,
			ExpectedAfterRevision:       after.ConfigurationRevision,
			RuntimeIdentityRevision:     snapshot.RuntimeIdentityRevision,
			CapabilityResolverRevision:  snapshot.CapabilityResolverRevision,
			EndpointProviderID:          endpoint.ProviderID, EndpointID: endpoint.EndpointID,
			EndpointRevision: endpoint.EndpointRevision, ChangedFields: changed,
			ExpiresAt: now.Add(previewLifetime).UTC(),
		}
		preview.Digest = previewDigestWithEndpoint(preview, endpoint)
		preview.PreviewID = preview.Digest
		return nil
	})
	if err != nil {
		return FallbackPatchPreviewV1{}, normalizeAdapterError(err, ErrorDatabase)
	}
	return preview, nil
}

func (s *Service) loadInboundState(tx *gorm.DB, inboundID uint) (model.Inbound, int64, InboundFallbackSnapshotV1, error) {
	if inboundID == 0 {
		return model.Inbound{}, 0, InboundFallbackSnapshotV1{}, adapterFailure(ErrorUnsupportedConfig)
	}
	var inbound model.Inbound
	if err := tx.Preload("Tls").First(&inbound, inboundID).Error; err != nil {
		return model.Inbound{}, 0, InboundFallbackSnapshotV1{}, adapterFailure(ErrorDatabase)
	}
	var referenceCount int64
	if inbound.TlsId != 0 {
		if err := tx.Model(&model.Inbound{}).Where("tls_id = ?", inbound.TlsId).Count(&referenceCount).Error; err != nil {
			return model.Inbound{}, 0, InboundFallbackSnapshotV1{}, adapterFailure(ErrorDatabase)
		}
	}
	probeCtx := tx.Statement.Context
	if probeCtx == nil {
		probeCtx = context.Background()
	}
	expectedOptionsDigest, err := s.currentInboundOptionsDigest(probeCtx, tx, &inbound)
	if err != nil {
		return model.Inbound{}, 0, InboundFallbackSnapshotV1{}, adapterFailure(ErrorUnsupportedConfig)
	}
	counts, err := authenticationCountsDB(probeCtx, tx, []uint{inbound.Id})
	if err != nil {
		return model.Inbound{}, 0, InboundFallbackSnapshotV1{}, adapterFailure(ErrorDatabase)
	}
	snapshot := buildSnapshotWithRuntimeDigest(inbound, referenceCount, s.identity, s.effective, counts[inbound.Id], expectedOptionsDigest)
	return inbound, referenceCount, snapshot, nil
}

func (s *Service) checkPreviewExpectations(snapshot InboundFallbackSnapshotV1, request PreviewFallbackPatchRequestV1) error {
	expected := request.Expected
	if s.identity.State != RuntimeIdentityVerified || expected.RuntimeIdentityRevision != s.identity.IdentityRevision {
		return adapterFailure(ErrorUnsupportedRuntime)
	}
	if expected.CapabilityResolverRevision != CapabilityResolverRevisionV1 ||
		snapshot.CapabilityResolverRevision != expected.CapabilityResolverRevision {
		return adapterFailure(ErrorStalePreview)
	}
	if snapshot.InboundDatabaseID != expected.InboundDatabaseID || snapshot.ResourceID != expected.ResourceID ||
		snapshot.ConfigurationRevision != expected.ConfigurationRevision {
		return adapterFailure(ErrorStalePreview)
	}
	if containsReason(snapshot.ReasonCodes, ReasonInboundOptionsMalformed) || containsReason(snapshot.ReasonCodes, ReasonInboundShapeUnknown) ||
		containsReason(snapshot.ReasonCodes, ReasonTLSOptionsMalformed) || containsReason(snapshot.ReasonCodes, ReasonTLSReferenceMissing) ||
		containsReason(snapshot.ReasonCodes, ReasonTLSReferenceMismatch) {
		return adapterFailure(ErrorUnsupportedConfig)
	}
	switch request.Variant {
	case FallbackPatchVLESSRealityHandshakeTCP:
		if snapshot.Type != "vless" || snapshot.Capability.Disposition != CapabilitySupportedNaturalFallback ||
			snapshot.Capability.Variant != NativeFallbackVLESSRealityTCP {
			return adapterFailure(ErrorUnsupportedConfig)
		}
		if snapshot.TLSReferenceCount != 1 {
			return adapterFailure(ErrorSharedTLS)
		}
		if request.ReplaceDefaultToo {
			return adapterFailure(ErrorUnsupportedConfig)
		}
	case FallbackPatchTrojanDefaultTCP:
		if snapshot.Type != "trojan" || snapshot.Capability.Disposition != CapabilitySupported || !snapshot.DefaultFallback.Present {
			return adapterFailure(ErrorUnsupportedConfig)
		}
		if request.ReplaceDefaultToo {
			return adapterFailure(ErrorUnsupportedConfig)
		}
	case FallbackPatchTrojanALPNTCP:
		if snapshot.Type != "trojan" || snapshot.Capability.Disposition != CapabilitySupported || len(snapshot.TLS.ALPN) == 0 ||
			len(snapshot.ALPNFallbacks) != len(snapshot.TLS.ALPN) {
			return adapterFailure(ErrorUnsupportedConfig)
		}
	default:
		return adapterFailure(ErrorUnsupportedConfig)
	}
	return nil
}

type candidateCheckpointData struct {
	PreviousTarget  *checkpointTargetV1
	PreviousALPN    []checkpointALPNTargetV1
	PreviousDefault *checkpointTargetV1
}

func (s *Service) candidateFor(ctx context.Context, tx *gorm.DB, inbound model.Inbound, referenceCount int64, request PreviewFallbackPatchRequestV1) (model.Inbound, []ChangedFieldV1, candidateCheckpointData, string, error) {
	candidate := inbound
	if inbound.Tls != nil {
		tlsCopy := *inbound.Tls
		tlsCopy.Server = append(json.RawMessage(nil), inbound.Tls.Server...)
		candidate.Tls = &tlsCopy
	}
	candidate.Options = append(json.RawMessage(nil), inbound.Options...)
	target := checkpointTargetV1{Server: request.ApprovedEndpoint.Bind, Port: request.ApprovedEndpoint.Port}
	var changed []ChangedFieldV1
	var checkpoint candidateCheckpointData
	var err error
	switch request.Variant {
	case FallbackPatchVLESSRealityHandshakeTCP:
		if candidate.Tls == nil || referenceCount != 1 {
			return model.Inbound{}, nil, checkpoint, "", adapterFailure(ErrorSharedTLS)
		}
		var previous checkpointTargetV1
		candidate.Tls.Server, previous, err = patchRealityHandshake(candidate.Tls.Server, target)
		checkpoint.PreviousTarget = &previous
		changed = []ChangedFieldV1{{Path: "tls.reality.handshake.server"}, {Path: "tls.reality.handshake.server_port"}}
	case FallbackPatchTrojanDefaultTCP:
		var previous checkpointTargetV1
		candidate.Options, previous, err = patchTrojanFallback(candidate.Options, target)
		checkpoint.PreviousTarget = &previous
		changed = []ChangedFieldV1{{Path: "fallback"}}
	case FallbackPatchTrojanALPNTCP:
		alpn := buildSnapshot(inbound, referenceCount, s.identity, nil).TLS.ALPN
		for _, protocol := range alpn {
			if !containsExactString(request.ApprovedEndpoint.ApplicationProtocols, protocol) {
				return model.Inbound{}, nil, checkpoint, "", adapterFailure(ErrorInvalidEndpoint)
			}
		}
		candidate.Options, checkpoint.PreviousALPN, checkpoint.PreviousDefault, err = patchTrojanALPN(candidate.Options, alpn, target, request.ReplaceDefaultToo)
		changed = []ChangedFieldV1{{Path: "fallback_for_alpn"}}
		if request.ReplaceDefaultToo {
			changed = append(changed, ChangedFieldV1{Path: "fallback"})
		}
	default:
		err = adapterFailure(ErrorUnsupportedConfig)
	}
	if err != nil {
		return model.Inbound{}, nil, checkpoint, "", normalizeAdapterError(err, ErrorInvalidCandidate)
	}
	content, err := candidate.MarshalJSON()
	if err != nil {
		return model.Inbound{}, nil, checkpoint, "", adapterFailure(ErrorInvalidCandidate)
	}
	if s.mutation.Hydrator != nil {
		content, err = s.mutation.Hydrator.HydrateInbound(ctx, tx, &candidate, content)
		if err != nil {
			return model.Inbound{}, nil, checkpoint, "", adapterFailure(ErrorInvalidCandidate)
		}
	}
	validator := s.mutation.validator
	if validator == nil {
		validator = defaultCandidateValidator{}
	}
	if err = validator.ValidateInbound(ctx, content); err != nil {
		return model.Inbound{}, nil, checkpoint, "", adapterFailure(ErrorInvalidCandidate)
	}
	expectedOptionsDigest, err := canonicalInboundOptionsDigest(ctx, content)
	if err != nil {
		return model.Inbound{}, nil, checkpoint, "", adapterFailure(ErrorInvalidCandidate)
	}
	return candidate, changed, checkpoint, expectedOptionsDigest, nil
}

func canonicalInboundOptionsDigest(ctx context.Context, content []byte) (string, error) {
	parseContext := sb.Context(ctx, coreregistry.InboundRegistry(), coreregistry.OutboundRegistry(),
		coreregistry.EndpointRegistry(), coreregistry.DNSTransportRegistry(), coreregistry.ServiceRegistry())
	var inbound option.Inbound
	if err := inbound.UnmarshalJSONContext(parseContext, content); err != nil {
		return "", err
	}
	canonical, err := inbound.MarshalJSONContext(parseContext)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func validateApprovedEndpoint(variant FallbackPatchVariantV1, endpoint ApprovedEndpointV1) (ApprovedEndpointV1, error) {
	if !safeOpaque(endpoint.ProviderID) || !safeOpaque(endpoint.EndpointID) || !safeRevision(endpoint.EndpointRevision) ||
		strings.ToLower(strings.TrimSpace(endpoint.Network)) != "tcp" || !endpoint.Local || endpoint.Port == 0 {
		return ApprovedEndpointV1{}, adapterFailure(ErrorInvalidEndpoint)
	}
	address, err := netip.ParseAddr(strings.TrimSpace(endpoint.Bind))
	if err != nil || !address.IsLoopback() {
		return ApprovedEndpointV1{}, adapterFailure(ErrorInvalidEndpoint)
	}
	endpoint.Bind = address.Unmap().String()
	expectedFamily := "ipv6"
	if address.Unmap().Is4() {
		expectedFamily = "ipv4"
	}
	if strings.ToLower(strings.TrimSpace(endpoint.AddressFamily)) != expectedFamily {
		return ApprovedEndpointV1{}, adapterFailure(ErrorInvalidEndpoint)
	}
	security := strings.ToLower(strings.TrimSpace(endpoint.TransportSecurity))
	protocols := exactStrings(endpoint.ApplicationProtocols)
	switch variant {
	case FallbackPatchVLESSRealityHandshakeTCP:
		if security != "tls" {
			return ApprovedEndpointV1{}, adapterFailure(ErrorInvalidEndpoint)
		}
	case FallbackPatchTrojanDefaultTCP, FallbackPatchTrojanALPNTCP:
		if security != "none" || !containsExactString(protocols, "http/1.1") {
			return ApprovedEndpointV1{}, adapterFailure(ErrorInvalidEndpoint)
		}
	default:
		return ApprovedEndpointV1{}, adapterFailure(ErrorUnsupportedConfig)
	}
	endpoint.Network = "tcp"
	endpoint.AddressFamily = expectedFamily
	endpoint.TransportSecurity = security
	endpoint.ApplicationProtocols = protocols
	return endpoint, nil
}

func previewDigestWithEndpoint(preview FallbackPatchPreviewV1, endpoint ApprovedEndpointV1) string {
	return digestValue(struct {
		Schema, ResourceID, Variant, Before, After, Runtime, Resolver, Provider, Endpoint, EndpointRevision, EndpointBinding string
		InboundID                                                                                                            uint
		Changed                                                                                                              []ChangedFieldV1
		Warnings                                                                                                             []ReasonCode
		ExpiresAtUnix                                                                                                        int64
	}{preview.Schema, preview.ResourceID, string(preview.Variant), preview.BeforeConfigurationRevision,
		preview.ExpectedAfterRevision, preview.RuntimeIdentityRevision, preview.CapabilityResolverRevision,
		preview.EndpointProviderID, preview.EndpointID, preview.EndpointRevision, endpointBindingDigest(endpoint),
		preview.InboundDatabaseID, preview.ChangedFields, preview.WarningCodes, preview.ExpiresAt.Unix()})
}

func endpointBindingDigest(endpoint ApprovedEndpointV1) string {
	return digestValue(struct {
		Schema, ProviderID, EndpointID, EndpointRevision, Network, AddressFamily, Bind, TransportSecurity string
		Port                                                                                              uint16
		Local                                                                                             bool
		ApplicationProtocols                                                                              []string
	}{"solovey-ui/approved-endpoint-binding/v1", endpoint.ProviderID, endpoint.EndpointID,
		endpoint.EndpointRevision, endpoint.Network, endpoint.AddressFamily, endpoint.Bind,
		endpoint.TransportSecurity, endpoint.Port, endpoint.Local, endpoint.ApplicationProtocols})
}

func (s *Service) now() time.Time {
	if s != nil && s.mutation.now != nil {
		return s.mutation.now().UTC()
	}
	return time.Now().UTC()
}

func safeOpaque(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t") {
		return false
	}
	return true
}

func safeRevision(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func containsExactString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func adapterFailure(code AdapterErrorCode) error {
	return &AdapterError{Code: code}
}

func normalizeAdapterError(err error, fallback AdapterErrorCode) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*AdapterError); ok {
		return err
	}
	return adapterFailure(fallback)
}
