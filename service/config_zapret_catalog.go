//go:build !minimal

package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/settings/store"
	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"gorm.io/gorm"
)

type ZapretCatalogView struct {
	Catalog        strategy.Catalog `json:"catalog"`
	Revision       string           `json:"revision"`
	References     []ZapretStatus   `json:"references"`
	Ready          bool             `json:"ready"`
	TransferSchema string           `json:"transferSchema"`
	EngineCommit   string           `json:"engineCommit"`
	EngineLicense  string           `json:"engineLicense"`
}

type ZapretStatus struct {
	outbounds.ZapretReference
	DesiredRevision string `json:"desiredRevision,omitempty"`
	AppliedRevision string `json:"appliedRevision,omitempty"`
	State           string `json:"state"`
}

type ZapretCatalogChange struct {
	Action           string             `json:"action"`
	ExpectedRevision string             `json:"expectedRevision"`
	ID               string             `json:"id,omitempty"`
	Draft            *strategy.Draft    `json:"draft,omitempty"`
	Transfer         *strategy.Transfer `json:"transfer,omitempty"`
}

type ZapretDiagnosticRequest struct {
	OutboundRef      string `json:"outboundRef"`
	StrategyRef      string `json:"strategyRef"`
	ExpectedRevision string `json:"expectedRevision"`
}

func (s *ConfigService) DiagnoseZapret(ctx context.Context, input ZapretDiagnosticRequest) (result coreruntime.CheckOutboundResult, err error) {
	manager := s.runtime().restart()
	if manager == nil {
		return result, errors.New("strategy configuration lifecycle is unavailable")
	}
	err = manager.RunBlocking(func() error {
		view, err := s.zapretCatalogLocked()
		if err != nil {
			return err
		}
		if !view.Ready || view.Revision != input.ExpectedRevision {
			return errors.New("Zapret batch is unavailable or the strategy catalog changed")
		}
		for _, ref := range view.References {
			if ref.State == "applied" && ref.OutboundRef == input.OutboundRef && ref.StrategyRef == input.StrategyRef {
				result = s.CheckOutboundWithSource(ctx, ref.OutboundRef, "https://www.gstatic.com/generate_204", coreruntime.ProbeSourceDiagnostic)
				return nil
			}
		}
		return errors.New("selected Zapret binding is no longer enabled")
	})
	return result, err
}

func readZapretCatalog(db *gorm.DB) (strategy.Catalog, error) {
	catalog := strategy.Catalog{Schema: strategy.CatalogSchema, Records: make(map[string]strategy.Record)}
	raw, err := outbounds.ReadZapretCatalog(db)
	if err != nil || raw == nil {
		return catalog, err
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return catalog, err
	}
	if err := catalog.Validate(); err != nil {
		return catalog, err
	}
	for id := range catalog.Records {
		if !zapretactivation.ManagedStrategyRef(id) {
			return catalog, errors.New("non-editor strategy ID in the managed catalog")
		}
	}
	return catalog, nil
}

func (s *ConfigService) ZapretCatalog() (ZapretCatalogView, error) {
	manager := s.runtime().restart()
	if manager == nil {
		return ZapretCatalogView{}, errors.New("strategy configuration lifecycle is unavailable")
	}
	var view ZapretCatalogView
	err := manager.RunBlocking(func() error {
		var err error
		view, err = s.zapretCatalogLocked()
		return err
	})
	return view, err
}

func (s *ConfigService) zapretCatalogLocked() (ZapretCatalogView, error) {
	db := configDatabase()
	if db == nil {
		return ZapretCatalogView{}, errors.New("configuration database is not initialized")
	}
	view := ZapretCatalogView{Ready: s.runtime().zapretComponentStarted(), TransferSchema: strategy.TransferSchema,
		EngineCommit: strategy.EngineCommit, EngineLicense: strategy.EngineLicense}
	var refs []outbounds.ZapretReference
	var wanted []zapretactivation.Request
	err := db.Transaction(func(tx *gorm.DB) error {
		catalog, err := readZapretCatalog(tx)
		if err != nil {
			return err
		}
		view.Catalog, view.Revision = catalog, catalog.Revision()
		refs, err = outbounds.ZapretReferences(tx)
		if err != nil {
			return err
		}
		// An unresolved or unqualified desired set must still be visible.
		wanted, _ = outbounds.ZapretRequests(tx)
		return nil
	})
	if err != nil {
		return view, err
	}
	var applied []zapretactivation.AppliedBinding
	core := s.coreInstance()
	if batch := s.runtime().zapretBatch; batch != "" && core != nil && core.IsRunning() && view.Ready {
		client := broker.NewClient(broker.RolePanel)
		_, err := client.Invoke(context.Background(), broker.Call{Verb: broker.VerbZapretObserve, OperationID: "zapret-observe",
			Payload: zapretactivation.BatchIdentity{BatchID: batch}, Timeout: time.Second}, &applied)
		if err != nil {
			applied = nil
		}
	}
	view.References = make([]ZapretStatus, 0, len(refs))
	for _, ref := range refs {
		status := ZapretStatus{ZapretReference: ref, State: "off", DesiredRevision: view.Catalog.Records[ref.StrategyRef].Definition.Revision}
		if ref.Enabled {
			status.State = "blocked"
			for _, observation := range applied {
				if observation.OutboundRef != ref.OutboundRef {
					continue
				}
				status.AppliedRevision = observation.StrategyRevision
				if slices.Contains(wanted, observation.Request) && (status.DesiredRevision == "" || status.DesiredRevision == observation.StrategyRevision) {
					status.State = "applied"
				} else {
					status.State = "pending"
				}
			}
		}
		view.References = append(view.References, status)
	}
	return view, nil
}

func (s *ConfigService) SaveZapretCatalog(change ZapretCatalogChange, actor string) (ZapretCatalogView, error) {
	draft, err := change.selectedDraft()
	if err != nil {
		return ZapretCatalogView{}, err
	}
	manager := s.runtime().restart()
	if manager == nil {
		return ZapretCatalogView{}, errors.New("strategy configuration lifecycle is unavailable")
	}
	data, err := json.Marshal(map[string]string{"strategyRef": change.ID, "catalogRevision": change.ExpectedRevision})
	if err != nil {
		return ZapretCatalogView{}, err
	}
	err = manager.RunBlocking(func() error {
		_, err := s.saveWithMutation(zapretCatalogObject, change.Action, data, actor, true,
			func(tx *gorm.DB, plan *configSavePlan) error { return saveZapretCatalog(tx, plan, change, draft) })
		return err
	})
	if err != nil {
		return ZapretCatalogView{}, err
	}
	return s.ZapretCatalog()
}

func (c *ZapretCatalogChange) selectedDraft() (strategy.Draft, error) {
	var draft strategy.Draft
	if len(c.ExpectedRevision) != 64 || c.ID != "" && !zapretactivation.ManagedStrategyRef(c.ID) {
		return draft, errors.New("missing catalog revision or invalid strategy identity")
	}
	switch c.Action {
	case "delete":
		if c.ID != "" && c.Draft == nil && c.Transfer == nil {
			return draft, nil
		}
	case "create", "edit", "import":
		if c.Action == "import" {
			if c.ID != "" || c.Draft != nil || c.Transfer == nil {
				break
			}
			var err error
			draft, err = c.Transfer.Import()
			if err != nil {
				return draft, err
			}
		} else {
			if c.Draft == nil || c.Transfer != nil || c.Draft.ID != "" || (c.Action == "edit") != (c.ID != "") {
				break
			}
			draft = *c.Draft
		}
		if c.ID == "" {
			var id [16]byte
			if _, err := rand.Read(id[:]); err != nil {
				return draft, err
			}
			c.ID = zapretactivation.ManagedStrategyPrefix + hex.EncodeToString(id[:])
		}
		draft.ID = c.ID
		return draft, nil
	}
	return draft, errors.New("invalid Zapret strategy change")
}

func saveZapretCatalog(tx *gorm.DB, plan *configSavePlan, change ZapretCatalogChange, draft strategy.Draft) error {
	catalog, err := readZapretCatalog(tx)
	if err != nil {
		return err
	}
	if catalog.Revision() != change.ExpectedRevision {
		return errors.New("Zapret strategy catalog changed; reload before saving")
	}
	previous, exists := catalog.Records[change.ID]
	if exists != (change.Action == "edit" || change.Action == "delete") {
		return errors.New("Zapret strategy identity changed; reload before saving")
	}
	refs, err := outbounds.ZapretReferences(tx)
	if err != nil {
		return err
	}
	if change.Action == "delete" {
		for _, ref := range refs {
			if ref.StrategyRef == change.ID {
				return fmt.Errorf("strategy is referenced by Direct %q; clear or reassign its binding first", ref.OutboundRef)
			}
		}
		delete(catalog.Records, change.ID)
	} else {
		next, err := strategy.SealDraft(draft)
		if err != nil {
			return err
		}
		catalog.Records[change.ID] = next
		for _, ref := range refs {
			if ref.Enabled && ref.StrategyRef == change.ID && next.Definition.Revision != previous.Definition.Revision {
				plan.MergeOutboundChange(&singboxapply.Change{NeedsRestart: true, QuiesceCore: true, RestartReason: "A bound Zapret strategy changed"})
				break
			}
		}
	}
	if err := catalog.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(catalog)
	if err != nil || len(data) > zapretactivation.MaxCatalogBytes {
		return errors.New("Zapret strategy catalog exceeds its storage limit")
	}
	return store.UpsertValue(tx, zapretactivation.CatalogKey, string(data))
}
