//go:build !minimal

package fallbackhtml

import (
	"context"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	neutral "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/fallback-html/authority"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	"gorm.io/gorm"
)

const (
	providerTargetFreshness = 90 * time.Second
	providerObservationStep = 30 * time.Second
	providerPressureSlots   = uint32(3)
)

func (p targetProvider) InventoryV2(ctx context.Context, request neutral.InventoryV2Request) (neutral.InventoryV2Result, *neutral.ProviderContractError) {
	if err := request.Validate(); err != nil {
		return neutral.InventoryV2Result{}, providerError(neutral.ProviderErrorInvalid, "inventory_request_invalid")
	}
	if err := contextError(ctx); err != nil {
		return neutral.InventoryV2Result{}, err
	}
	if p.db == nil {
		return neutral.InventoryV2Result{}, providerError(neutral.ProviderErrorUnavailable, "provider_database_unavailable")
	}
	result := neutral.InventoryV2Result{Targets: []neutral.FallbackTargetV2{}}
	var sites []fallbackdomain.Site
	query := p.db.WithContext(ctx).
		Preload("Targets", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Preload("Publishes", func(tx *gorm.DB) *gorm.DB { return tx.Where("active = ?", true).Order("id ASC") }).
		Preload("Publishes.Files", func(tx *gorm.DB) *gorm.DB { return tx.Order("public_path ASC, id ASC") }).
		Preload("Publishes.Redirects", func(tx *gorm.DB) *gorm.DB { return tx.Order("from_path ASC, id ASC") }).
		Where("enabled = ? AND status = ?", true, "published").Order("id ASC")
	if err := query.Find(&sites).Error; err != nil {
		return neutral.InventoryV2Result{}, providerDatabaseError(ctx)
	}
	now := p.providerNow()
	capacityByTarget, _, capacityErr := authority.GuardingCounts(p.db.WithContext(ctx), id, now)
	if capacityErr != nil {
		result.ReasonCodes = []string{"reservation_authority_invalid"}
		return result, nil
	}
	for _, site := range sites {
		if uint32(len(result.Targets)) >= request.Limit {
			result.Truncated = true
			break
		}
		used := capacityByTarget["site:"+strconv.FormatUint(uint64(site.ID), 10)]
		target, reason, err := p.targetV2FromSite(ctx, p.db.WithContext(ctx), site, now, &used)
		if err != nil {
			if contextError(ctx) != nil {
				return neutral.InventoryV2Result{}, contextError(ctx)
			}
			result.ReasonCodes = append(result.ReasonCodes, "reservation_authority_invalid")
			continue
		}
		if reason != "" {
			result.ReasonCodes = append(result.ReasonCodes, reason)
			continue
		}
		result.Targets = append(result.Targets, target)
	}
	result.ReasonCodes = canonicalProviderReasons(result.ReasonCodes)
	return result, nil
}

func (p targetProvider) ResolveV2(ctx context.Context, reference neutral.FallbackTargetReferenceV2) (neutral.ResolveV2Result, *neutral.ProviderContractError) {
	if err := reference.Validate(); err != nil || reference.ProviderID != id {
		return neutral.ResolveV2Result{}, providerError(neutral.ProviderErrorInvalid, "target_reference_invalid")
	}
	if err := contextError(ctx); err != nil {
		return neutral.ResolveV2Result{}, err
	}
	if p.db == nil {
		return neutral.ResolveV2Result{}, providerError(neutral.ProviderErrorUnavailable, "provider_database_unavailable")
	}
	target, err := p.resolveTargetV2(ctx, p.db.WithContext(ctx), reference, p.providerNow())
	if err != nil {
		return neutral.ResolveV2Result{}, classifyTargetError(ctx, err)
	}
	return neutral.ResolveV2Result{Target: target}, nil
}

func (p targetProvider) Reserve(ctx context.Context, request neutral.ReserveRequestV1) (neutral.ReservationResultV1, *neutral.ProviderContractError) {
	if err := request.Validate(); err != nil || request.ExactTargetReference.ProviderID != id {
		return neutral.ReservationResultV1{}, providerError(neutral.ProviderErrorInvalid, "reserve_request_invalid")
	}
	return p.withReservationWrite(ctx, request.RequestID, authority.RequestRevision(request), func(tx *gorm.DB, now time.Time) (neutral.ProviderTargetReservationV1, error) {
		target, err := p.resolveTargetV2(ctx, tx, request.ExactTargetReference, now)
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		used, err := authority.CountGuardingTarget(tx, id, target.Identity.TargetID, now)
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		if err := neutral.ValidateReservationCapacity(target, used, now); err != nil {
			return neutral.ProviderTargetReservationV1{}, providerError(neutral.ProviderErrorCapacity, "target_capacity_exhausted")
		}
		globalUsed, err := authority.CountAllGuarding(tx, now)
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		if globalUsed >= neutral.MaxReservationsV2 {
			return neutral.ProviderTargetReservationV1{}, providerError(neutral.ProviderErrorCapacity, "provider_capacity_exhausted")
		}
		reservationID, err := authority.NewOpaqueID("fhr-")
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		revision, err := authority.NewOpaqueID("r-")
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		reservation := neutral.ProviderTargetReservationV1{
			Schema: neutral.ProviderTargetReservationSchemaV1, ReservationID: reservationID,
			ReservationRevision: revision, HolderID: request.HolderID, Purpose: request.Purpose,
			ExactTargetReference: request.ExactTargetReference, State: neutral.ReservationReserved,
			IssuedAt: now.Unix(), RenewedAt: now.Unix(),
			FreshnessExpiresAt: now.Add(time.Duration(request.FreshnessDurationSecs) * time.Second).Unix(),
		}
		if err := reservation.ValidateAt(now); err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		row, err := authority.EncodeReservation(reservation, now.Unix(), now.Unix())
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		return reservation, nil
	})
}

func (p targetProvider) FenceForMutation(ctx context.Context, request neutral.ReservationMutationRequestV1) (neutral.ReservationResultV1, *neutral.ProviderContractError) {
	if err := request.Validate(false); err != nil {
		return neutral.ReservationResultV1{}, providerError(neutral.ProviderErrorInvalid, "fence_request_invalid")
	}
	return p.transitionReservation(ctx, request.RequestID, authority.RequestRevision(request), request.ReservationID, request.ExpectedRevision, func(current neutral.ProviderTargetReservationV1, now time.Time) (neutral.ProviderTargetReservationV1, error) {
		next := current
		next.State = neutral.ReservationMutationPending
		revision, err := authority.NewOpaqueID("r-")
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		next.ReservationRevision = revision
		return next, neutral.ValidateReservationTransition(current, next, reservationCAS(request), neutral.ReservationMutationFence, now)
	})
}

func (p targetProvider) Activate(ctx context.Context, request neutral.ReservationMutationRequestV1) (neutral.ReservationResultV1, *neutral.ProviderContractError) {
	if err := request.Validate(true); err != nil {
		return neutral.ReservationResultV1{}, providerError(neutral.ProviderErrorInvalid, "activate_request_invalid")
	}
	return p.transitionReservation(ctx, request.RequestID, authority.RequestRevision(request), request.ReservationID, request.ExpectedRevision, func(current neutral.ProviderTargetReservationV1, now time.Time) (neutral.ProviderTargetReservationV1, error) {
		next := current
		next.State = neutral.ReservationActive
		next.RenewedAt = now.Unix()
		next.FreshnessExpiresAt = now.Add(time.Duration(request.FreshnessDurationSecs) * time.Second).Unix()
		revision, err := authority.NewOpaqueID("r-")
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		next.ReservationRevision = revision
		return next, neutral.ValidateReservationTransition(current, next, reservationCAS(request), neutral.ReservationMutationActivate, now)
	})
}

func (p targetProvider) Renew(ctx context.Context, request neutral.ReservationMutationRequestV1) (neutral.ReservationResultV1, *neutral.ProviderContractError) {
	if err := request.Validate(true); err != nil {
		return neutral.ReservationResultV1{}, providerError(neutral.ProviderErrorInvalid, "renew_request_invalid")
	}
	return p.transitionReservation(ctx, request.RequestID, authority.RequestRevision(request), request.ReservationID, request.ExpectedRevision, func(current neutral.ProviderTargetReservationV1, now time.Time) (neutral.ProviderTargetReservationV1, error) {
		renewedAt := now.Unix()
		if renewedAt <= current.RenewedAt {
			renewedAt = current.RenewedAt + 1
		}
		next := current
		next.RenewedAt = renewedAt
		next.FreshnessExpiresAt = renewedAt + int64(request.FreshnessDurationSecs)
		revision, err := authority.NewOpaqueID("r-")
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		next.ReservationRevision = revision
		return next, neutral.ValidateReservationTransition(current, next, reservationCAS(request), neutral.ReservationMutationRenew, time.Unix(renewedAt, 0).UTC())
	})
}

func (p targetProvider) Release(ctx context.Context, request neutral.ReleaseReservationRequestV1) (neutral.ReservationResultV1, *neutral.ProviderContractError) {
	if err := request.Validate(); err != nil {
		return neutral.ReservationResultV1{}, providerError(neutral.ProviderErrorInvalid, "release_request_invalid")
	}
	return p.transitionReservation(ctx, request.RequestID, authority.RequestRevision(request), request.ReservationID, request.ExpectedRevision, func(current neutral.ProviderTargetReservationV1, now time.Time) (neutral.ProviderTargetReservationV1, error) {
		next := current
		next.State = neutral.ReservationReleased
		next.ReleasedAt = now.Unix()
		if next.ReleasedAt < next.RenewedAt {
			next.ReleasedAt = next.RenewedAt
		}
		revision, err := authority.NewOpaqueID("r-")
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		next.ReservationRevision = revision
		return next, neutral.ValidateReservationReleaseTransition(current, next, request, now)
	})
}

func (p targetProvider) GetReservation(ctx context.Context, request neutral.GetReservationRequestV1) (neutral.ReservationResultV1, *neutral.ProviderContractError) {
	if strings.TrimSpace(request.ReservationID) == "" || len(request.ReservationID) > neutral.MaxOpaqueIDLengthV2 {
		return neutral.ReservationResultV1{}, providerError(neutral.ProviderErrorInvalid, "reservation_id_invalid")
	}
	if err := contextError(ctx); err != nil {
		return neutral.ReservationResultV1{}, err
	}
	if p.db == nil {
		return neutral.ReservationResultV1{}, providerError(neutral.ProviderErrorUnavailable, "provider_database_unavailable")
	}
	var row authority.ReservationModel
	err := p.db.WithContext(ctx).Where("reservation_id = ?", request.ReservationID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return neutral.ReservationResultV1{}, providerError(neutral.ProviderErrorNotFound, "reservation_not_found")
	}
	if err != nil {
		return neutral.ReservationResultV1{}, providerDatabaseError(ctx)
	}
	reservation, err := authority.DecodeReservation(row)
	if err != nil {
		return neutral.ReservationResultV1{}, providerError(neutral.ProviderErrorInternal, "reservation_record_invalid")
	}
	return neutral.ReservationResultV1{Reservation: reservation}, nil
}

func (p targetProvider) ListReservations(ctx context.Context, query neutral.ListReservationsQueryV1) (neutral.ListReservationsResultV1, *neutral.ProviderContractError) {
	if err := query.Validate(); err != nil || (query.ProviderID != "" && query.ProviderID != id) {
		return neutral.ListReservationsResultV1{}, providerError(neutral.ProviderErrorInvalid, "reservation_query_invalid")
	}
	if err := contextError(ctx); err != nil {
		return neutral.ListReservationsResultV1{}, err
	}
	if p.db == nil {
		return neutral.ListReservationsResultV1{}, providerError(neutral.ProviderErrorUnavailable, "provider_database_unavailable")
	}
	db := p.db.WithContext(ctx).Where("provider_id = ?", id)
	if query.TargetID != "" {
		db = db.Where("target_id = ?", query.TargetID)
	}
	if query.HolderID != "" {
		db = db.Where("holder_id = ?", query.HolderID)
	}
	if query.State != "" {
		db = db.Where("state = ?", string(query.State))
	}
	if query.Continuation != "" {
		db = db.Where("reservation_id > ?", query.Continuation)
	}
	var rows []authority.ReservationModel
	if err := db.Order("reservation_id ASC").Limit(int(query.Limit) + 1).Find(&rows).Error; err != nil {
		return neutral.ListReservationsResultV1{}, providerDatabaseError(ctx)
	}
	result := neutral.ListReservationsResultV1{Reservations: []neutral.ProviderTargetReservationV1{}}
	if len(rows) > int(query.Limit) {
		rows = rows[:query.Limit]
		result.Truncated = true
	}
	for _, row := range rows {
		reservation, err := authority.DecodeReservation(row)
		if err != nil {
			return neutral.ListReservationsResultV1{}, providerError(neutral.ProviderErrorInternal, "reservation_record_invalid")
		}
		result.Reservations = append(result.Reservations, reservation)
	}
	if result.Truncated && len(result.Reservations) > 0 {
		result.Continuation = result.Reservations[len(result.Reservations)-1].ReservationID
	}
	return result, nil
}

func (p targetProvider) withReservationWrite(ctx context.Context, requestID, requestRevision string, write func(*gorm.DB, time.Time) (neutral.ProviderTargetReservationV1, error)) (neutral.ReservationResultV1, *neutral.ProviderContractError) {
	if p.db == nil {
		return neutral.ReservationResultV1{}, providerError(neutral.ProviderErrorUnavailable, "provider_database_unavailable")
	}
	var result neutral.ProviderTargetReservationV1
	err := authority.WithMutationLockContext(ctx, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			replay, found, err := authority.LoadReplay(tx, requestID, requestRevision)
			if err != nil {
				return err
			}
			if found {
				result = replay
				return nil
			}
			now := p.providerNow()
			result, err = write(tx, now)
			if err != nil {
				return err
			}
			return authority.SaveReplay(tx, requestID, requestRevision, result, now.Unix())
		})
	})
	if err != nil {
		return neutral.ReservationResultV1{}, classifyReservationError(ctx, err)
	}
	return neutral.ReservationResultV1{Reservation: result}, nil
}

func (p targetProvider) transitionReservation(ctx context.Context, requestID, requestRevision, reservationID, expectedRevision string, transition func(neutral.ProviderTargetReservationV1, time.Time) (neutral.ProviderTargetReservationV1, error)) (neutral.ReservationResultV1, *neutral.ProviderContractError) {
	return p.withReservationWrite(ctx, requestID, requestRevision, func(tx *gorm.DB, now time.Time) (neutral.ProviderTargetReservationV1, error) {
		var row authority.ReservationModel
		if err := tx.WithContext(ctx).Where("reservation_id = ?", reservationID).First(&row).Error; err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		current, err := authority.DecodeReservation(row)
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		if current.ReservationRevision != expectedRevision {
			return neutral.ProviderTargetReservationV1{}, providerError(neutral.ProviderErrorConflict, "reservation_revision_stale")
		}
		next, err := transition(current, now)
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		if next.ReservationRevision == "" {
			return neutral.ProviderTargetReservationV1{}, errors.New("reservation_revision_generation_failed")
		}
		updated, err := authority.EncodeReservation(next, row.CreatedAt, now.Unix())
		if err != nil {
			return neutral.ProviderTargetReservationV1{}, err
		}
		write := tx.WithContext(ctx).Model(&authority.ReservationModel{}).
			Where("reservation_id = ? AND reservation_revision = ?", current.ReservationID, current.ReservationRevision).
			Select("reservation_revision", "state", "renewed_at", "freshness_expires_at", "released_at", "reason_codes_json", "updated_at").Updates(updated)
		if write.Error != nil {
			return neutral.ProviderTargetReservationV1{}, write.Error
		}
		if write.RowsAffected != 1 {
			return neutral.ProviderTargetReservationV1{}, providerError(neutral.ProviderErrorConflict, "reservation_revision_stale")
		}
		return next, nil
	})
}

func (p targetProvider) resolveTargetV2(ctx context.Context, db *gorm.DB, reference neutral.FallbackTargetReferenceV2, now time.Time) (neutral.FallbackTargetV2, error) {
	siteID, ok := parseSiteTargetID(reference.TargetID)
	if !ok {
		return neutral.FallbackTargetV2{}, providerError(neutral.ProviderErrorNotFound, "target_not_found")
	}
	var site fallbackdomain.Site
	err := db.WithContext(ctx).
		Preload("Targets", func(tx *gorm.DB) *gorm.DB { return tx.Order("id ASC") }).
		Preload("Publishes", func(tx *gorm.DB) *gorm.DB { return tx.Where("active = ?", true).Order("id ASC") }).
		Preload("Publishes.Files", func(tx *gorm.DB) *gorm.DB { return tx.Order("public_path ASC, id ASC") }).
		Preload("Publishes.Redirects", func(tx *gorm.DB) *gorm.DB { return tx.Order("from_path ASC, id ASC") }).
		Where("enabled = ? AND status = ?", true, "published").First(&site, siteID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return neutral.FallbackTargetV2{}, providerError(neutral.ProviderErrorNotFound, "target_not_found")
	}
	if err != nil {
		return neutral.FallbackTargetV2{}, err
	}
	target, reason, err := p.targetV2FromSite(ctx, db, site, now, nil)
	if err != nil {
		return neutral.FallbackTargetV2{}, err
	}
	if reason != "" {
		return neutral.FallbackTargetV2{}, providerError(neutral.ProviderErrorUnavailable, reason)
	}
	if err := neutral.ResolveExactV2(reference, target, now); err != nil {
		return neutral.FallbackTargetV2{}, err
	}
	return target, nil
}

func (p targetProvider) targetV2FromSite(ctx context.Context, db *gorm.DB, site fallbackdomain.Site, now time.Time, knownUsed *uint32) (neutral.FallbackTargetV2, string, error) {
	if err := ctx.Err(); err != nil {
		return neutral.FallbackTargetV2{}, "", err
	}
	if len(site.Publishes) != 1 {
		return neutral.FallbackTargetV2{}, "publish_fact_ambiguous", nil
	}
	publish := site.Publishes[0]
	_, managementReachability, redirectReason := publishedRedirectFacts(publish)
	if managementReachability != hostresources.CapabilityNo {
		return neutral.FallbackTargetV2{}, redirectReason, nil
	}
	target, warnings, err := siteResourceTarget(site)
	if err != nil || len(warnings) != 0 || len(site.Targets) != 1 {
		return neutral.FallbackTargetV2{}, "runtime_target_ambiguous", nil
	}
	if !strings.EqualFold(target.Kind, "standalone") || !strings.EqualFold(target.Runtime, "gin") || target.RootPath != "/" {
		return neutral.FallbackTargetV2{}, "runtime_target_unsupported", nil
	}
	normalized := hostresources.NormalizeListen(target.Listen)
	if normalized.Class != hostresources.ListenLoopback || target.Port <= 0 || target.Port > 65535 {
		return neutral.FallbackTargetV2{}, "endpoint_not_isolated_loopback", nil
	}
	address := net.ParseIP(normalized.Value)
	if address == nil || !address.IsLoopback() {
		return neutral.FallbackTargetV2{}, "endpoint_address_invalid", nil
	}
	addressFamily := hostresources.AddressFamilyIPv6
	canonicalAddress := address.String()
	if address.To4() != nil {
		addressFamily = hostresources.AddressFamilyIPv4
		canonicalAddress = address.To4().String()
	}
	protocols := []neutral.ApplicationProtocol{neutral.ApplicationProtocolHTTP11}
	transportSecurity := neutral.TransportSecurityPlaintext
	serverNames := []string{}
	mode := "plaintext"
	if target.TLS {
		transportSecurity = neutral.TransportSecurityTLS
		protocols = append(protocols, neutral.ApplicationProtocolHTTP2)
		mode = "tls"
		host := canonicalServerName(target.Host)
		if host == "" {
			return neutral.FallbackTargetV2{}, "tls_server_name_invalid", nil
		}
		verified := p.tlsServerNameVerified
		if verified == nil {
			verified = fallbackservice.RuntimeTLSAcceptsServerName
		}
		if !verified(host) {
			return neutral.FallbackTargetV2{}, "tls_server_name_unverified", nil
		}
		serverNames = append(serverNames, host)
	}
	status := fallbackservice.RuntimeStatus{}
	healthStatus := fallbackservice.RuntimeHealth{}
	readiness := neutral.ReadinessUnknown
	healthReasons := []string{"runtime_status_unknown"}
	if p.runtime != nil {
		status = p.runtime.Status()
		healthStatus = p.runtime.Health(publicsurface.Context{AdminBasePath: "/app/"})
		readiness = neutral.ReadinessNotReady
		healthReasons = []string{"runtime_target_not_ready"}
		if status.Active && status.SiteID == site.ID && healthStatus.OK && p.runtime.Owns(canonicalAddress, target.Port) {
			readiness = neutral.ReadinessReady
			healthReasons = nil
		}
	}
	targetID := "site:" + strconv.FormatUint(uint64(site.ID), 10)
	used := uint32(0)
	if knownUsed != nil {
		used = *knownUsed
	} else {
		used, err = authority.CountGuardingTarget(db.WithContext(ctx), id, targetID, now)
		if err != nil {
			return neutral.FallbackTargetV2{}, "", err
		}
	}
	capacityState := neutral.CapacityReady
	capacityReasons := []string(nil)
	switch {
	case used >= authority.SlotsPerTarget:
		capacityState = neutral.CapacityExhausted
		capacityReasons = []string{"reservation_capacity_exhausted"}
	case used >= providerPressureSlots:
		capacityState = neutral.CapacityPressured
		capacityReasons = []string{"reservation_capacity_pressured"}
	}
	providerRevision := hostresources.Revision(struct {
		Provider       string
		Slots          uint32
		Pressure       uint32
		RedirectPolicy string
	}{id, authority.SlotsPerTarget, providerPressureSlots, publishedRedirectPolicyRevision})
	observed := now.Truncate(providerObservationStep)
	result, err := neutral.FinalizeFallbackTargetV2(neutral.FallbackTargetV2{
		Identity: neutral.TargetIdentity{ProviderID: id, TargetID: targetID},
		Publish:  neutral.PublishFactsV2{Revision: publish.Version, ContentDigest: publishDigestV2(publish)},
		Endpoint: neutral.EndpointV2{
			EndpointID: "site:" + strconv.FormatUint(uint64(site.ID), 10) + ":target:" + strconv.FormatUint(uint64(target.ID), 10) + ":" + mode,
			Network:    hostresources.NetworkTCP, AddressFamily: addressFamily, Address: canonicalAddress,
			Port: uint16(target.Port), Local: true, TransportSecurity: transportSecurity,
			ApplicationProtocols: protocols, AcceptedServerNames: serverNames,
			ProxyProtocol: hostresources.CapabilityNo, CanReachManagement: managementReachability,
		},
		Health:           neutral.HealthV2{Readiness: readiness, ObservedAt: observed.Unix(), ExpiresAt: observed.Add(providerTargetFreshness).Unix(), ReasonCodes: healthReasons},
		Capacity:         neutral.CapacityV2{State: capacityState, ReservationSlotsTotal: authority.SlotsPerTarget, ReservationSlotsUsed: used, ObservedAt: observed.Unix(), ExpiresAt: observed.Add(providerTargetFreshness).Unix(), ReasonCodes: capacityReasons},
		ProviderRevision: providerRevision, Source: "fallback-html-runtime", ConfidenceBP: 10000,
	})
	if err != nil {
		return neutral.FallbackTargetV2{}, "target_fact_invalid", nil
	}
	return result, "", nil
}

func (p targetProvider) providerNow() time.Time {
	if p.now != nil {
		return p.now().UTC()
	}
	return time.Now().UTC()
}

func reservationCAS(request neutral.ReservationMutationRequestV1) neutral.ReservationCASV1 {
	return neutral.ReservationCASV1{RequestID: request.RequestID, ReservationID: request.ReservationID, ExpectedRevision: request.ExpectedRevision}
}

func parseSiteTargetID(value string) (uint, bool) {
	if !strings.HasPrefix(value, "site:") {
		return 0, false
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, "site:"), 10, 32)
	return uint(parsed), err == nil && parsed > 0
}

func canonicalServerName(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(value, ".")))
	if value == "" || len(value) > 253 || net.ParseIP(value) != nil {
		return ""
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ""
		}
		for _, character := range label {
			if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
				continue
			}
			return ""
		}
	}
	return value
}

func canonicalProviderReasons(values []string) []string {
	sort.Strings(values)
	out := values[:0]
	for _, value := range values {
		if value == "" || (len(out) > 0 && out[len(out)-1] == value) {
			continue
		}
		out = append(out, value)
	}
	if len(out) > neutral.MaxReasonCodesV2 {
		out = out[:neutral.MaxReasonCodesV2]
	}
	return out
}

func providerError(class neutral.ProviderErrorClass, reason string) *neutral.ProviderContractError {
	return &neutral.ProviderContractError{Class: class, ReasonCode: reason}
}

func contextError(ctx context.Context) *neutral.ProviderContractError {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return providerError(neutral.ProviderErrorTimeout, "provider_request_timeout")
	}
	return providerError(neutral.ProviderErrorUnavailable, "provider_request_cancelled")
}

func providerDatabaseError(ctx context.Context) *neutral.ProviderContractError {
	if err := contextError(ctx); err != nil {
		return err
	}
	return providerError(neutral.ProviderErrorInternal, "provider_storage_failure")
}

func classifyTargetError(ctx context.Context, err error) *neutral.ProviderContractError {
	if contractError := (*neutral.ProviderContractError)(nil); errors.As(err, &contractError) {
		return contractError
	}
	if contextError(ctx) != nil {
		return contextError(ctx)
	}
	switch err.Error() {
	case "fallback_target_reference_v2_stale":
		return providerError(neutral.ProviderErrorStale, "target_reference_stale")
	case "fallback_target_health_not_actionable":
		return providerError(neutral.ProviderErrorUnavailable, "target_health_not_actionable")
	case "fallback_target_capacity_not_actionable", "fallback_target_v2_has_unresolved_reasons":
		return providerError(neutral.ProviderErrorCapacity, "target_capacity_not_actionable")
	default:
		return providerDatabaseError(ctx)
	}
}

func classifyReservationError(ctx context.Context, err error) *neutral.ProviderContractError {
	if contractError := (*neutral.ProviderContractError)(nil); errors.As(err, &contractError) {
		return contractError
	}
	if contextError(ctx) != nil {
		return contextError(ctx)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return providerError(neutral.ProviderErrorNotFound, "reservation_not_found")
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "replay_conflict"):
		return providerError(neutral.ProviderErrorConflict, "reservation_replay_conflict")
	case strings.Contains(message, "cas_stale"), strings.Contains(message, "transition_conflict"):
		return providerError(neutral.ProviderErrorConflict, "reservation_revision_stale")
	case strings.Contains(message, "reconcile_required"), strings.Contains(message, "terminal"):
		return providerError(neutral.ProviderErrorConflict, "reservation_state_conflict")
	case strings.Contains(message, "capacity"):
		return providerError(neutral.ProviderErrorCapacity, "target_capacity_exhausted")
	case strings.Contains(message, "invalid"), strings.Contains(message, "illegal"), strings.Contains(message, "proof_required"):
		return providerError(neutral.ProviderErrorInvalid, "reservation_transition_invalid")
	default:
		return providerDatabaseError(ctx)
	}
}
