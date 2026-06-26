package importxui

import (
	"context"
	"fmt"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/importxui/mapping"
	"github.com/MalenkiySolovey/solovey-ui/database/importxui/source"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"

	"gorm.io/gorm"
)

func Apply(srcPath string, plan MigrationPlan, opts ApplyOptions) (*Report, error) {
	opts = opts.normalized()
	report := &Report{}
	if !applyMu.TryLock() {
		return report, fmt.Errorf("xui-import: %w", ErrBusy)
	}
	defer applyMu.Unlock()
	if err := checkContext(opts.Context); err != nil {
		return report, fmt.Errorf("xui-import: %w", err)
	}
	hash, err := source.Hash(srcPath)
	if err != nil {
		return report, fmt.Errorf("xui-import: %w", err)
	}
	if plan.Source.Hash != "" && plan.Source.Hash != hash {
		return report, fmt.Errorf("xui-import: %w", ErrPlanStale)
	}
	src, err := source.Open(srcPath)
	if err != nil {
		return report, fmt.Errorf("xui-import: %w", err)
	}
	defer src.Close()
	db := dbsqlite.DB()
	if db == nil {
		return report, fmt.Errorf("xui-import: destination database is not initialized")
	}
	var backupPath string
	if !opts.DryRun && !opts.SkipBackup {
		now := time.Now().Unix()
		if opts.Now != nil {
			now = opts.Now()
		}
		backupPath, err = WritePreImportBackup(now)
		if err != nil {
			return report, err
		}
		report.BackupPath = backupPath
	}
	tx := db.Begin()
	if tx.Error != nil {
		return report, fmt.Errorf("xui-import: %w", tx.Error)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback().Error
		}
	}()
	state := &applyState{
		report:           report,
		plan:             normalizePlan(plan),
		realityByKey:     map[string]*mapping.RealitySpec{},
		realityBySource:  map[int64]*mapping.RealitySpec{},
		plainTLSByKey:    map[string]*mapping.TLSCertSpec{},
		plainTLSBySource: map[int64]*mapping.TLSCertSpec{},
		tlsIDByKey:       map[string]uint{},
		inboundIDBySrc:   map[int64]uint{},
		server:           destinationServer(tx),
		onProgress:       opts.OnProgress,
		total:            countRunnableItems(plan),
		hostname:         resolveLinkHostname(tx, opts.Hostname),
	}
	if err := state.run(opts.Context, tx, src, opts); err != nil {
		return report, fmt.Errorf("xui-import: %w", err)
	}
	if opts.DryRun {
		return report, nil
	}
	if err := tx.Commit().Error; err != nil {
		return report, fmt.Errorf("xui-import: %w", err)
	}
	committed = true
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return report, fmt.Errorf("xui-import: %w", err)
	}
	return report, nil
}

type applyState struct {
	report           *Report
	plan             map[string]PlanItem
	realityByKey     map[string]*mapping.RealitySpec
	realityBySource  map[int64]*mapping.RealitySpec
	plainTLSByKey    map[string]*mapping.TLSCertSpec
	plainTLSBySource map[int64]*mapping.TLSCertSpec
	tlsIDByKey       map[string]uint
	inboundIDBySrc   map[int64]uint
	clientRefs       []mapping.ClientRef
	server           string
	hostname         string
	onProgress       func(Progress)
	current          int
	total            int
}

func (s *applyState) run(ctx context.Context, tx *gorm.DB, src *source.Database, opts ApplyOptions) error {
	total, err := src.InboundCount()
	if err != nil {
		return err
	}
	s.report.Summary.Inbounds.Total = total
	if err := s.applyTLS(ctx, tx, src); err != nil {
		return err
	}
	if err := s.applyInboundsEndpoints(ctx, tx, src); err != nil {
		return err
	}
	if err := s.applyClients(ctx, tx, src); err != nil {
		return err
	}
	if err := s.applySettings(ctx, tx, src); err != nil {
		return err
	}
	if err := s.applyAdmins(ctx, tx, src); err != nil {
		return err
	}
	if err := s.applyHistorical(ctx, tx, src, opts); err != nil {
		return err
	}
	if err := s.applyRouting(ctx, tx, src, opts); err != nil {
		return err
	}
	if !opts.DryRun && !opts.SkipAudit {
		if err := recordAuditWithBackup(tx, s.report, opts); err != nil {
			return err
		}
		s.progress("audit", "xui_import")
	}
	return nil
}

func (s *applyState) applyTLS(ctx context.Context, tx *gorm.DB, src *source.Database) error {
	return src.EachInbound(func(row source.InboundRow) error {
		if err := checkContext(ctx); err != nil {
			return err
		}
		spec, warnings, err := mapping.ExtractReality(row)
		if err != nil {
			return err
		}
		if spec == nil {
			return s.applyPlainTLS(tx, row)
		}
		if existing, ok := s.realityByKey[spec.Key]; ok {
			s.realityBySource[row.ID] = existing
			s.report.Summary.TLS.Reused++
			return nil
		}
		s.realityByKey[spec.Key] = spec
		s.realityBySource[row.ID] = spec
		item := s.item(KindTLS, spec.Key)
		s.report.warnAll(warnings)
		if item.Action == ActionSkip {
			// Skip means "don't create/overwrite", not "unlink": if a matching
			// TLS record already exists, still resolve its id so inbounds keep a
			// valid reference instead of being saved with TlsId=0.
			if existing, found, err := mapping.FindExistingRealityTLS(tx, *spec); err != nil {
				return err
			} else if found {
				s.tlsIDByKey[spec.Key] = existing.Id
			}
			return nil
		}
		record, err := mapping.BuildTLSRecord(*spec)
		if err != nil {
			return err
		}
		if item.DstTag != "" {
			record.Name = item.DstTag
		}
		existing, found, err := mapping.FindExistingRealityTLS(tx, *spec)
		if err != nil {
			return err
		}
		if found && item.Action != ActionReplace {
			s.tlsIDByKey[spec.Key] = existing.Id
			s.report.Summary.TLS.Reused++
			s.progress("tls", record.Name)
			return nil
		}
		if found && item.Action == ActionReplace {
			if err := tx.Delete(&existing).Error; err != nil {
				return err
			}
			record.SortOrder = existing.SortOrder
		} else {
			sortOrder, err := nextImportSortOrder(tx, &model.Tls{})
			if err != nil {
				return err
			}
			record.SortOrder = sortOrder
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		s.tlsIDByKey[spec.Key] = record.Id
		s.report.Summary.TLS.Created++
		s.progress("tls", record.Name)
		return nil
	})
}

// applyPlainTLS mirrors the reality TLS apply path for a non-reality inbound
// whose certificate is inline: it dedups by certificate content, honours the
// plan item's action (skip/replace), and records the resulting TLS id so the
// inbound can reference it.
func (s *applyState) applyPlainTLS(tx *gorm.DB, row source.InboundRow) error {
	spec, warnings, err := mapping.ExtractPlainTLS(row)
	if err != nil {
		return err
	}
	s.report.warnAll(warnings)
	if spec == nil {
		return nil
	}
	if existing, ok := s.plainTLSByKey[spec.Key]; ok {
		s.plainTLSBySource[row.ID] = existing
		s.report.Summary.TLS.Reused++
		return nil
	}
	s.plainTLSByKey[spec.Key] = spec
	s.plainTLSBySource[row.ID] = spec
	item := s.item(KindTLS, spec.Key)
	if item.Action == ActionSkip {
		// Skip means "don't create/overwrite", not "unlink": resolve an existing
		// matching record's id so referencing inbounds keep a valid TlsId.
		if existing, found, err := mapping.FindExistingPlainTLS(tx, *spec); err != nil {
			return err
		} else if found {
			s.tlsIDByKey[spec.Key] = existing.Id
		}
		return nil
	}
	record, err := mapping.BuildPlainTLSRecord(*spec)
	if err != nil {
		return err
	}
	if item.DstTag != "" {
		record.Name = item.DstTag
	}
	existing, found, err := mapping.FindExistingPlainTLS(tx, *spec)
	if err != nil {
		return err
	}
	if found && item.Action != ActionReplace {
		s.tlsIDByKey[spec.Key] = existing.Id
		s.report.Summary.TLS.Reused++
		s.progress("tls", record.Name)
		return nil
	}
	if found && item.Action == ActionReplace {
		if err := tx.Delete(&existing).Error; err != nil {
			return err
		}
		record.SortOrder = existing.SortOrder
	} else {
		sortOrder, err := nextImportSortOrder(tx, &model.Tls{})
		if err != nil {
			return err
		}
		record.SortOrder = sortOrder
	}
	if err := tx.Create(&record).Error; err != nil {
		return err
	}
	s.tlsIDByKey[spec.Key] = record.Id
	s.report.Summary.TLS.Created++
	s.progress("tls", record.Name)
	return nil
}

func (s *applyState) applyInboundsEndpoints(ctx context.Context, tx *gorm.DB, src *source.Database) error {
	return src.EachInbound(func(row source.InboundRow) error {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if row.Protocol == "wireguard" {
			endpoint, warnings, err := mapping.MapWireguardEndpoint(row)
			if err != nil {
				return err
			}
			s.report.warnAll(warnings)
			item := s.item(KindEndpoint, row.ID)
			if endpoint == nil || item.Action == ActionSkip {
				s.report.Summary.Endpoints.Skipped++
				return nil
			}
			if item.DstTag != "" {
				endpoint.Tag = item.DstTag
			}
			imported, err := applyEndpointAction(tx, endpoint, item.Action, s.report)
			if err != nil {
				return err
			}
			if imported {
				s.report.Summary.Endpoints.Imported++
			}
			s.progress("endpoints", endpoint.Tag)
			return nil
		}
		var tlsID uint
		var reality *mapping.RealitySpec
		if spec, ok := s.realityBySource[row.ID]; ok {
			reality = spec
			tlsID = s.tlsIDByKey[spec.Key]
		} else if spec, ok := s.plainTLSBySource[row.ID]; ok {
			tlsID = s.tlsIDByKey[spec.Key]
		}
		mapped, err := mapping.MapInbound(row, tlsID, reality, s.server)
		if err != nil {
			return err
		}
		s.report.warnAll(mapped.Warnings)
		item := s.item(KindInbound, row.ID)
		if mapped.Inbound.Type == "" || item.Action == ActionSkip {
			s.report.Summary.Inbounds.Skipped++
			return nil
		}
		if item.DstTag != "" {
			mapped.Inbound.Tag = item.DstTag
		}
		dstID, imported, skipped, err := applyInboundAction(tx, &mapped.Inbound, item.Action, s.report)
		if err != nil {
			return err
		}
		if skipped {
			s.report.Summary.Inbounds.Skipped++
			return nil
		}
		if imported {
			s.report.Summary.Inbounds.Imported++
		}
		s.inboundIDBySrc[row.ID] = dstID
		for i := range mapped.ClientRefs {
			mapped.ClientRefs[i].DstInboundID = dstID
		}
		s.clientRefs = append(s.clientRefs, mapped.ClientRefs...)
		s.report.ByInbound = append(s.report.ByInbound, InboundStat{
			SrcTag:  row.Tag,
			DstTag:  mapped.Inbound.Tag,
			Clients: len(mapped.ClientRefs),
		})
		s.progress("inbounds", mapped.Inbound.Tag)
		return nil
	})
}

func (s *applyState) applyClients(ctx context.Context, tx *gorm.DB, src *source.Database) error {
	aggs, err := collectClientAggregates(src, s.clientRefs, s.inboundIDBySrc)
	if err != nil {
		return err
	}
	s.report.Summary.Clients.UniqueEmails = len(aggs)
	emails := make([]string, 0, len(aggs))
	for email := range aggs {
		emails = append(emails, email)
	}
	sortStrings(emails)
	for _, email := range emails {
		if err := checkContext(ctx); err != nil {
			return err
		}
		item := s.item(KindClient, email)
		if item.Action == ActionSkip {
			continue
		}
		if item.DstTag != "" && item.DstTag != email {
			renameAggregate(aggs[email], item.DstTag)
		}
		if err := applyClientAction(tx, aggs[email], item.Action, s.report, s.hostname); err != nil {
			return err
		}
		s.progress("clients", item.DstTag)
	}
	return nil
}
