package importxui

import (
	"context"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"gorm.io/gorm"
)

type importState struct {
	report           *Report
	realityByKey     map[string]*realitySpec
	realityBySource  map[int64]*realitySpec
	plainTLSByKey    map[string]*tlsCertSpec
	plainTLSBySource map[int64]*tlsCertSpec
	tlsIDByKey       map[string]uint
	inboundIDBySrc   map[int64]uint
	clientRefs       []ClientRef
	server           string
	hostname         string
}

func Import(srcPath string, opts Options) (*Report, error) {
	report := &Report{}
	opts, err := opts.normalized()
	if err != nil {
		return report, fmt.Errorf("xui-import: %w", err)
	}
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	if err := checkContext(opts.Context); err != nil {
		return report, fmt.Errorf("xui-import: %w", err)
	}
	if !opts.DryRun {
		if !applyMu.TryLock() {
			return report, fmt.Errorf("xui-import: %w", ErrBusy)
		}
		defer applyMu.Unlock()
	}
	src, err := openSource(srcPath)
	if err != nil {
		return report, fmt.Errorf("xui-import: %w", err)
	}
	defer src.close()

	db := database.GetDB()
	if db == nil {
		return report, fmt.Errorf("xui-import: destination database is not initialized")
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

	state := &importState{
		report:           report,
		realityByKey:     map[string]*realitySpec{},
		realityBySource:  map[int64]*realitySpec{},
		plainTLSByKey:    map[string]*tlsCertSpec{},
		plainTLSBySource: map[int64]*tlsCertSpec{},
		tlsIDByKey:       map[string]uint{},
		inboundIDBySrc:   map[int64]uint{},
		server:           destinationServer(tx),
		hostname:         resolveLinkHostname(tx, opts.Hostname),
	}
	if err := state.run(tx, src, opts); err != nil {
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

func (s *importState) run(tx *gorm.DB, src *sourceDB, opts Options) error {
	total, err := src.inboundCount()
	if err != nil {
		return err
	}
	s.report.Summary.Inbounds.Total = total
	if err := s.importTLS(tx, src); err != nil {
		return err
	}
	if err := s.importInboundsAndEndpoints(tx, src, opts.Strategy); err != nil {
		return err
	}
	if err := s.importClients(tx, src, opts.Strategy); err != nil {
		return err
	}
	if err := s.importOptionalExtras(tx, src, opts); err != nil {
		return err
	}
	if !opts.DryRun && !opts.SkipAudit {
		if err := s.recordAudit(tx, opts); err != nil {
			return err
		}
	}
	return nil
}

func (s *importState) importOptionalExtras(tx *gorm.DB, src *sourceDB, opts Options) error {
	if !opts.IncludeHistory && !opts.IncludeRouting {
		return nil
	}
	items := map[string]PlanItem{}
	if opts.IncludeHistory {
		items[planKey(KindHistory, "traffic")] = PlanItem{Kind: KindHistory, SrcID: "traffic", DstTag: "stats", Action: ActionCreate}
	}
	if opts.IncludeRouting {
		items[planKey(KindRouting, "xrayConfig")] = PlanItem{Kind: KindRouting, SrcID: "xrayConfig", DstTag: "config", Action: ActionCreate}
	}
	extra := &applyState{
		report:     s.report,
		plan:       items,
		onProgress: opts.OnProgress,
		total:      len(items),
	}
	if opts.IncludeHistory {
		if err := extra.applyHistorical(opts.Context, tx, src, ApplyOptions{Context: opts.Context, Now: opts.Now}); err != nil {
			return err
		}
	}
	if opts.IncludeRouting {
		if err := extra.applyRouting(opts.Context, tx, src, ApplyOptions{Context: opts.Context, Now: opts.Now}); err != nil {
			return err
		}
	}
	return nil
}
