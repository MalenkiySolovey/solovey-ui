package coreinboundcontrol

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

type Service struct {
	db        *gorm.DB
	effective EffectiveInboundReader
	identity  CoreRuntimeIdentityV1
	mutation  MutationDependencies
}

func New(db *gorm.DB, effective EffectiveInboundReader) *Service {
	return NewWithMutations(db, effective, MutationDependencies{})
}

func NewWithMutations(db *gorm.DB, effective EffectiveInboundReader, mutation MutationDependencies) *Service {
	service := &Service{db: db, effective: effective, identity: ReadRuntimeIdentityV1(), mutation: mutation}
	return service
}

func (s *Service) Identity(context.Context) CoreRuntimeIdentityV1 {
	if s == nil {
		return ResolveRuntimeIdentityV1(RuntimeBuildInputV1{})
	}
	return s.identity
}

func (s *Service) Snapshot(ctx context.Context, inboundID uint) (InboundFallbackSnapshotV1, error) {
	if s == nil || s.db == nil {
		return InboundFallbackSnapshotV1{}, fmt.Errorf("inbound database is unavailable")
	}
	var inbound model.Inbound
	if err := s.db.WithContext(ctx).Preload("Tls").First(&inbound, inboundID).Error; err != nil {
		return InboundFallbackSnapshotV1{}, fmt.Errorf("load inbound: %w", err)
	}
	var referenceCount int64
	if inbound.TlsId != 0 {
		if err := s.db.WithContext(ctx).Model(&model.Inbound{}).Where("tls_id = ?", inbound.TlsId).Count(&referenceCount).Error; err != nil {
			return InboundFallbackSnapshotV1{}, fmt.Errorf("count TLS references: %w", err)
		}
	}
	counts, err := s.authenticationCounts(ctx, []uint{inbound.Id})
	if err != nil {
		return InboundFallbackSnapshotV1{}, err
	}
	hydratedContent, expectedRuntimeDigest, err := s.expectedRuntimeOptions(ctx, &inbound)
	if err != nil {
		return InboundFallbackSnapshotV1{}, fmt.Errorf("derive effective inbound configuration: %w", err)
	}
	snapshot := buildSnapshotWithRuntimeDigest(inbound, referenceCount, s.identity, s.effective, counts[inbound.Id], expectedRuntimeDigest)
	if err := applyHydratedLocalProxyShape(&snapshot, hydratedContent); err != nil {
		return InboundFallbackSnapshotV1{}, fmt.Errorf("derive effective local proxy shape: %w", err)
	}
	return snapshot, nil
}

func (s *Service) ListSnapshots(ctx context.Context, limit int) ([]InboundFallbackSnapshotV1, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("inbound database is unavailable")
	}
	if limit <= 0 {
		return []InboundFallbackSnapshotV1{}, nil
	}
	var inbounds []model.Inbound
	if err := s.db.WithContext(ctx).Preload("Tls").Order("sort_order ASC, id ASC").Limit(limit).Find(&inbounds).Error; err != nil {
		return nil, fmt.Errorf("list inbounds: %w", err)
	}
	type referenceRow struct {
		TLSID uint  `gorm:"column:tls_id"`
		Count int64 `gorm:"column:reference_count"`
	}
	counts := make(map[uint]int64)
	var rows []referenceRow
	if err := s.db.WithContext(ctx).Model(&model.Inbound{}).Select("tls_id, COUNT(*) AS reference_count").Where("tls_id <> 0").Group("tls_id").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("count TLS references: %w", err)
	}
	for _, row := range rows {
		counts[row.TLSID] = row.Count
	}
	result := make([]InboundFallbackSnapshotV1, 0, len(inbounds))
	ids := make([]uint, 0, len(inbounds))
	for _, inbound := range inbounds {
		ids = append(ids, inbound.Id)
	}
	authCounts, err := s.authenticationCounts(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		hydratedContent, expectedRuntimeDigest, digestErr := s.expectedRuntimeOptions(ctx, &inbound)
		if digestErr != nil {
			return nil, fmt.Errorf("derive effective inbound configuration: %w", digestErr)
		}
		snapshot := buildSnapshotWithRuntimeDigest(inbound, counts[inbound.TlsId], s.identity, s.effective, authCounts[inbound.Id], expectedRuntimeDigest)
		if shapeErr := applyHydratedLocalProxyShape(&snapshot, hydratedContent); shapeErr != nil {
			return nil, fmt.Errorf("derive effective local proxy shape: %w", shapeErr)
		}
		result = append(result, snapshot)
	}
	return result, nil
}

func (s *Service) expectedRuntimeOptions(ctx context.Context, inbound *model.Inbound) ([]byte, string, error) {
	content, err := s.hydratedInboundContent(ctx, inbound)
	if err != nil {
		return nil, "", err
	}
	digest, err := canonicalInboundOptionsDigest(ctx, content)
	return content, digest, err
}

func (s *Service) hydratedInboundContent(ctx context.Context, inbound *model.Inbound) ([]byte, error) {
	content, err := inbound.MarshalJSON()
	if err != nil {
		return nil, err
	}
	if s.mutation.Hydrator != nil {
		content, err = s.mutation.Hydrator.HydrateInbound(ctx, s.db.WithContext(ctx), inbound, content)
		if err != nil {
			return nil, err
		}
	}
	return content, nil
}

func (s *Service) authenticationCounts(ctx context.Context, inboundIDs []uint) (map[uint]int, error) {
	return authenticationCountsDB(ctx, s.db, inboundIDs)
}

func authenticationCountsDB(ctx context.Context, db *gorm.DB, inboundIDs []uint) (map[uint]int, error) {
	result := make(map[uint]int, len(inboundIDs))
	if len(inboundIDs) == 0 {
		return result, nil
	}
	// Older bounded control-plane fixtures predate client membership storage.
	// An absent table is exact legacy zero membership; any error from an
	// existing table remains fail-closed below.
	if db == nil || !db.Migrator().HasTable(&model.Client{}) {
		return result, nil
	}
	wanted := make(map[uint]bool, len(inboundIDs))
	for _, id := range inboundIDs {
		wanted[id] = true
	}
	var clients []model.Client
	if err := db.WithContext(ctx).Select("inbounds").Find(&clients).Error; err != nil {
		return nil, fmt.Errorf("count inbound authentication principals: %w", err)
	}
	for _, client := range clients {
		var memberships []uint
		if err := json.Unmarshal(client.Inbounds, &memberships); err != nil {
			return nil, fmt.Errorf("decode inbound authentication membership: %w", err)
		}
		seen := map[uint]bool{}
		for _, id := range memberships {
			if wanted[id] && !seen[id] {
				result[id]++
				seen[id] = true
			}
		}
	}
	return result, nil
}
