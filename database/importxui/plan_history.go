package importxui

import (
	"context"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/importxui/source"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"

	"gorm.io/gorm"
)

func planHistorical(ctx context.Context, src *source.Database, plan *MigrationPlan) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	clients, err := src.Clients()
	if err != nil {
		return err
	}
	outbounds, err := src.OutboundTraffics()
	if err != nil {
		return err
	}
	count := 0
	for _, row := range clients {
		if row.Email != "" && (row.Up > 0 || row.Down > 0) {
			count++
		}
	}
	for _, row := range outbounds {
		if row.Tag != "" && (row.Up > 0 || row.Down > 0) {
			count++
		}
	}
	preview, err := marshalJSON(map[string]any{
		"client_traffics":   len(clients),
		"outbound_traffics": len(outbounds),
		"mode":              "aggregated_only",
	})
	if err != nil {
		return err
	}
	plan.Items = append(plan.Items, PlanItem{
		Kind:        KindHistory,
		SrcID:       "traffic",
		SrcTag:      "client_traffics/outbound_traffics",
		DstTag:      "stats",
		Action:      ActionCreate,
		PreviewJSON: preview,
		Warnings:    []string{"historical_aggregated_only"},
	})
	plan.Defaults.IncludeHistory = count > 0
	return nil
}

func (s *applyState) applyHistorical(ctx context.Context, tx *gorm.DB, src *source.Database, opts ApplyOptions) error {
	if !s.hasKind(KindHistory) {
		return nil
	}
	item := s.item(KindHistory, "traffic")
	if item.Action == ActionSkip {
		return nil
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	now := time.Now().Unix()
	if opts.Now != nil {
		now = opts.Now()
	}
	var stats []model.Stats
	clients, err := src.Clients()
	if err != nil {
		return err
	}
	for _, row := range clients {
		if row.Email == "" {
			continue
		}
		if row.Up > 0 {
			stats = append(stats, model.Stats{DateTime: now, Resource: "client", Tag: row.Email, Direction: true, Traffic: row.Up})
		}
		if row.Down > 0 {
			stats = append(stats, model.Stats{DateTime: now, Resource: "client", Tag: row.Email, Direction: false, Traffic: row.Down})
		}
	}
	outbounds, err := src.OutboundTraffics()
	if err != nil {
		return err
	}
	for _, row := range outbounds {
		if row.Tag == "" {
			continue
		}
		if row.Up > 0 {
			stats = append(stats, model.Stats{DateTime: now, Resource: "outbound", Tag: row.Tag, Direction: true, Traffic: row.Up})
		}
		if row.Down > 0 {
			stats = append(stats, model.Stats{DateTime: now, Resource: "outbound", Tag: row.Tag, Direction: false, Traffic: row.Down})
		}
	}
	if len(stats) > 0 {
		if err := dbsqlite.CreateInBatches(tx, &stats); err != nil {
			return err
		}
	}
	s.report.Summary.Historical.Total = len(stats)
	s.report.Summary.Historical.Imported = len(stats)
	s.report.warn("historical_aggregated_only")
	s.progress("historical", "stats")
	return nil
}
