//go:build !minimal

package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation/catalogstore"
	"gorm.io/gorm"
)

// ZapretCatalogSnapshot joins desired configuration with the broker's live
// observation under the Core lifecycle lock. The component owns its schema.
type ZapretCatalogSnapshot struct {
	Catalog    json.RawMessage
	References []outbounds.ZapretReference
	Desired    []zapretactivation.Request
	Applied    []zapretactivation.AppliedBinding
	Ready      bool
}

var errZapretCatalogLifecycle = errors.New("strategy configuration lifecycle is unavailable")

func (s *ConfigService) ZapretCatalog() (view ZapretCatalogSnapshot, err error) {
	manager := s.runtime().restart()
	if manager == nil {
		return view, errZapretCatalogLifecycle
	}
	err = manager.RunBlocking(func() error {
		var err error
		view, err = s.zapretCatalogLocked()
		return err
	})
	return view, err
}

func (s *ConfigService) zapretCatalogLocked() (view ZapretCatalogSnapshot, err error) {
	db := configDatabase()
	if db == nil {
		return view, errors.New("configuration database is not initialized")
	}
	view.Ready = s.runtime().zapretComponentStarted()
	err = db.Transaction(func(tx *gorm.DB) error {
		var err error
		view.Catalog, err = outbounds.ReadZapretCatalog(tx)
		if err != nil {
			return err
		}
		view.References, err = outbounds.ZapretReferences(tx)
		if err != nil {
			return err
		}
		// An unresolved or unqualified desired set must still be visible.
		view.Desired, _ = outbounds.ZapretRequests(tx)
		return nil
	})
	if err != nil {
		return view, err
	}
	core := s.coreInstance()
	if batch := s.runtime().zapretBatch; batch != "" && core != nil && core.IsRunning() && view.Ready {
		client := broker.NewClient(broker.RolePanel)
		// One read must account for both owned nftables tables, the bound
		// interfaces and the queue owner, including on a throttled broker.
		_, err := client.Invoke(context.Background(), broker.Call{Verb: broker.VerbZapretObserve, OperationID: "zapret-observe",
			Payload: zapretactivation.BatchIdentity{BatchID: batch}, Timeout: 3 * time.Second}, &view.Applied)
		if err != nil {
			view.Applied = nil
		}
	}
	return view, nil
}

func (s *ConfigService) DiagnoseZapret(ctx context.Context, outboundRef string, authorize func(ZapretCatalogSnapshot) error) (result coreruntime.CheckOutboundResult, err error) {
	manager := s.runtime().restart()
	if manager == nil || authorize == nil {
		return result, errZapretCatalogLifecycle
	}
	err = manager.RunBlocking(func() error {
		view, err := s.zapretCatalogLocked()
		if err != nil {
			return err
		}
		if err := authorize(view); err != nil {
			return err
		}
		result = s.CheckOutboundWithSource(ctx, outboundRef, "https://www.gstatic.com/generate_204", coreruntime.ProbeSourceDiagnostic)
		return nil
	})
	return result, err
}

// SaveZapretCatalog performs a component-owned mutation inside the ordinary
// config change transaction, including candidate preflight before quiescence.
func (s *ConfigService) SaveZapretCatalog(action, id, revision, actor string,
	mutate func(json.RawMessage, []outbounds.ZapretReference) (json.RawMessage, bool, error),
) error {
	manager := s.runtime().restart()
	if manager == nil || mutate == nil {
		return errZapretCatalogLifecycle
	}
	data, err := json.Marshal(map[string]string{"strategyRef": id, "catalogRevision": revision})
	if err != nil {
		return err
	}
	return manager.RunBlocking(func() error {
		_, err := s.saveWithMutation(zapretCatalogObject, action, data, actor, true,
			func(tx *gorm.DB, plan *configSavePlan) error {
				current, err := outbounds.ReadZapretCatalog(tx)
				if err != nil {
					return err
				}
				refs, err := outbounds.ZapretReferences(tx)
				if err != nil {
					return err
				}
				next, rebind, err := mutate(current, refs)
				if err != nil {
					return err
				}
				if rebind {
					plan.MergeOutboundChange(&singboxapply.Change{NeedsRestart: true, QuiesceCore: true, RestartReason: "A bound Zapret strategy changed"})
				}
				return catalogstore.Save(tx, current, next)
			})
		return err
	})
}
