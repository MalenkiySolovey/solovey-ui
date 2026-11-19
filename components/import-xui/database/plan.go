//go:build !minimal

package importxui

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/MalenkiySolovey/solovey-backend/operations/coordination"
	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	"github.com/MalenkiySolovey/solovey-component-import-xui/mapping"
	"github.com/MalenkiySolovey/solovey-component-import-xui/preview"
	"github.com/MalenkiySolovey/solovey-component-import-xui/source"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"gorm.io/gorm"
)

const (
	KindTLS      = preview.KindTLS
	KindInbound  = preview.KindInbound
	KindEndpoint = preview.KindEndpoint
	KindClient   = preview.KindClient
	KindSetting  = preview.KindSetting
	KindAdmin    = preview.KindAdmin
	KindHistory  = preview.KindHistory
	KindRouting  = preview.KindRouting

	ActionCreate  = preview.ActionCreate
	ActionMerge   = preview.ActionMerge
	ActionReplace = preview.ActionReplace
	ActionSkip    = preview.ActionSkip
)

var (
	ErrPlanStale = errors.New("plan_stale")
	ErrBusy      = errors.New("panel_import_busy")
	applyMu      sync.Mutex
)

type MigrationPlan = preview.Plan[UnsupportedObject]
type PlanDefaults = preview.PlanDefaults
type PlanSource = preview.PlanSource
type PlanItem = preview.Item[UnsupportedObject]

type Progress struct {
	Reference   *operationcoordination.Reference `json:"reference,omitempty"`
	Step        string                           `json:"step"`
	Current     int                              `json:"current"`
	Total       int                              `json:"total"`
	CurrentTag  string                           `json:"currentTag,omitempty"`
	CurrentName string                           `json:"currentName,omitempty"`
	Percent     int                              `json:"percent"`
}

type planningState struct {
	capabilities     entitycapabilities.Snapshot
	report           *Report
	realityByKey     map[string]*mapping.RealitySpec
	realityBySource  map[int64]*mapping.RealitySpec
	plainTLSByKey    map[string]*mapping.TLSCertSpec
	plainTLSBySource map[int64]*mapping.TLSCertSpec
	tlsIDByKey       map[string]uint
	inboundIDBySrc   map[int64]uint
	clientRefs       []mapping.ClientRef
	server           string
}

func Plan(srcPath string, opts PlanOptions) (*MigrationPlan, error) {
	opts, err := opts.normalized()
	if err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
	}
	if err := checkContext(opts.Context); err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
	}
	src, err := source.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
	}
	defer src.Close()
	hash, err := source.Hash(srcPath)
	if err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
	}
	db := dbsqlite.DB()
	if db == nil {
		return nil, fmt.Errorf("xui-import: destination database is not initialized")
	}
	tx := db.Session(&gorm.Session{})
	state := &planningState{
		capabilities:     targetCapabilities(opts.TargetCapabilities),
		report:           &Report{},
		realityByKey:     map[string]*mapping.RealitySpec{},
		realityBySource:  map[int64]*mapping.RealitySpec{},
		plainTLSByKey:    map[string]*mapping.TLSCertSpec{},
		plainTLSBySource: map[int64]*mapping.TLSCertSpec{},
		tlsIDByKey:       map[string]uint{},
		inboundIDBySrc:   map[int64]uint{},
		server:           destinationServer(tx),
	}
	plan := &MigrationPlan{
		Defaults: PlanDefaults{
			Strategy:        string(opts.Strategy),
			IncludeSettings: opts.IncludeSettings,
			AdminMode:       string(opts.AdminMode),
			OnlyNew:         opts.OnlyNew,
			IncludeHistory:  opts.IncludeHistory,
			IncludeRouting:  opts.IncludeRouting,
		},
		Source: PlanSource{
			Path: srcPath,
			Hash: hash,
		},
	}
	if err := state.planTLS(opts.Context, tx, src, plan, opts.Strategy); err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
	}
	if err := state.planInboundsEndpoints(opts.Context, tx, src, plan, opts.Strategy); err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
	}
	if err := state.planClients(opts.Context, tx, src, plan, opts.Strategy); err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
	}
	if opts.IncludeSettings {
		if err := planSettings(opts.Context, tx, src, plan, opts.Strategy); err != nil {
			return nil, fmt.Errorf("xui-import: %w", err)
		}
	}
	if opts.AdminMode != AdminModeSkip {
		if err := planAdmins(opts.Context, tx, src, plan, opts.Strategy, opts.AdminMode); err != nil {
			return nil, fmt.Errorf("xui-import: %w", err)
		}
	}
	if opts.IncludeHistory {
		if err := planHistorical(opts.Context, src, plan); err != nil {
			return nil, fmt.Errorf("xui-import: %w", err)
		}
	}
	if opts.IncludeRouting {
		if err := planRouting(opts.Context, src, plan, state.capabilities); err != nil {
			return nil, fmt.Errorf("xui-import: %w", err)
		}
	} else if err := planRoutingDisabledNotice(opts.Context, src, plan); err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
	}
	if err := preview.Classify(opts.Context, src, plan); err != nil {
		return nil, err
	}
	if opts.OnlyNew {
		markOnlyNew(plan)
	}
	return plan, nil
}

func defaultAction(conflict bool, strategy Strategy) string {
	if !conflict {
		return ActionCreate
	}
	switch strategy {
	case StrategyReplace:
		return ActionReplace
	case StrategySkip:
		return ActionSkip
	default:
		return ActionMerge
	}
}

func warningOnlyItem(kind string, srcID any, srcTag string, dstTag string, warnings []string) PlanItem {
	return PlanItem{
		Kind:        kind,
		SrcID:       srcID,
		SrcTag:      srcTag,
		DstTag:      dstTag,
		Action:      ActionSkip,
		PreviewJSON: json.RawMessage(`null`),
		Warnings:    warnings,
	}
}

func recordExists(tx *gorm.DB, modelValue any, query string, args ...any) (bool, error) {
	var count int64
	if err := tx.Model(modelValue).Where(query, args...).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
