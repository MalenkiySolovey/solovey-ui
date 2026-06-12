package importxui

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/database"

	"gorm.io/gorm"
)

const (
	KindTLS      = "tls"
	KindInbound  = "inbound"
	KindEndpoint = "endpoint"
	KindClient   = "client"
	KindSetting  = "setting"
	KindAdmin    = "admin"
	KindHistory  = "historical"
	KindRouting  = "routing"

	ActionCreate  = "create"
	ActionMerge   = "merge"
	ActionReplace = "replace"
	ActionSkip    = "skip"
)

var (
	ErrPlanStale = errors.New("plan_stale")
	ErrBusy      = errors.New("xui_import_busy")
	applyMu      sync.Mutex
)

type MigrationPlan struct {
	Items    []PlanItem   `json:"items"`
	Defaults PlanDefaults `json:"defaults"`
	Source   PlanSource   `json:"source"`
}

type PlanDefaults struct {
	Strategy        string `json:"strategy"`
	IncludeSettings bool   `json:"includeSettings"`
	AdminMode       string `json:"adminMode"`
	OnlyNew         bool   `json:"onlyNew"`
	IncludeHistory  bool   `json:"includeHistory"`
	IncludeRouting  bool   `json:"includeRouting"`
}

type PlanSource struct {
	Path string `json:"path,omitempty"`
	Hash string `json:"hash"`
}

type PlanItem struct {
	Kind        string          `json:"kind"`
	SrcID       any             `json:"srcId"`
	SrcTag      string          `json:"srcTag,omitempty"`
	DstTag      string          `json:"dstTag"`
	Action      string          `json:"action"`
	Conflict    bool            `json:"conflict"`
	AdminMode   string          `json:"adminMode,omitempty"`
	PreviewJSON json.RawMessage `json:"previewJson"`
	Warnings    []string        `json:"warnings,omitempty"`
}

type Progress struct {
	Step        string `json:"step"`
	Current     int    `json:"current"`
	Total       int    `json:"total"`
	CurrentTag  string `json:"currentTag,omitempty"`
	CurrentName string `json:"currentName,omitempty"`
	Percent     int    `json:"percent"`
}

func Plan(srcPath string, opts PlanOptions) (*MigrationPlan, error) {
	opts, err := opts.normalized()
	if err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
	}
	if err := checkContext(opts.Context); err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
	}
	src, err := openSource(srcPath)
	if err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
	}
	defer src.close()
	hash, err := hashSource(srcPath)
	if err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
	}
	db := database.GetDB()
	if db == nil {
		return nil, fmt.Errorf("xui-import: destination database is not initialized")
	}
	tx := db.Session(&gorm.Session{})
	state := &importState{
		report:           &Report{},
		realityByKey:     map[string]*realitySpec{},
		realityBySource:  map[int64]*realitySpec{},
		plainTLSByKey:    map[string]*tlsCertSpec{},
		plainTLSBySource: map[int64]*tlsCertSpec{},
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
		if err := planRouting(opts.Context, src, plan); err != nil {
			return nil, fmt.Errorf("xui-import: %w", err)
		}
	} else if err := planRoutingDisabledNotice(opts.Context, src, plan); err != nil {
		return nil, fmt.Errorf("xui-import: %w", err)
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
