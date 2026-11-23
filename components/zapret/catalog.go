//go:build !minimal

package zapret

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type catalogView struct {
	Catalog        strategy.Catalog `json:"catalog"`
	Revision       string           `json:"revision"`
	References     []bindingStatus  `json:"references"`
	Ready          bool             `json:"ready"`
	TransferSchema string           `json:"transferSchema"`
	EngineCommit   string           `json:"engineCommit"`
	EngineLicense  string           `json:"engineLicense"`
}

type bindingStatus struct {
	outbounds.ZapretReference
	DesiredRevision string `json:"desiredRevision,omitempty"`
	AppliedRevision string `json:"appliedRevision,omitempty"`
	State           string `json:"state"`
}

type catalogChange struct {
	Action           string             `json:"action"`
	ExpectedRevision string             `json:"expectedRevision"`
	ID               string             `json:"id,omitempty"`
	Draft            *strategy.Draft    `json:"draft,omitempty"`
	Transfer         *strategy.Transfer `json:"transfer,omitempty"`
}

type diagnosticRequest struct {
	OutboundRef      string `json:"outboundRef"`
	StrategyRef      string `json:"strategyRef"`
	ExpectedRevision string `json:"expectedRevision"`
}

func readCatalog(raw json.RawMessage) (strategy.Catalog, error) {
	catalog := strategy.Catalog{Schema: strategy.CatalogSchema, Records: make(map[string]strategy.Record)}
	if raw == nil {
		return catalog, nil
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

func makeCatalogView(snapshot service.ZapretCatalogSnapshot) (catalogView, error) {
	catalog, err := readCatalog(snapshot.Catalog)
	if err != nil {
		return catalogView{}, err
	}
	view := catalogView{Catalog: catalog, Revision: catalog.Revision(), Ready: snapshot.Ready,
		TransferSchema: strategy.TransferSchema, EngineCommit: strategy.EngineCommit, EngineLicense: strategy.EngineLicense}
	view.References = make([]bindingStatus, 0, len(snapshot.References))
	for _, ref := range snapshot.References {
		status := bindingStatus{ZapretReference: ref, State: "off", DesiredRevision: catalog.Records[ref.StrategyRef].Definition.Revision}
		if ref.Enabled {
			status.State = "blocked"
			for _, observation := range snapshot.Applied {
				if observation.OutboundRef != ref.OutboundRef {
					continue
				}
				status.AppliedRevision = observation.StrategyRevision
				if slices.Contains(snapshot.Desired, observation.Request) && (status.DesiredRevision == observation.StrategyRevision ||
					status.DesiredRevision == "" && !zapretactivation.ManagedStrategyRef(ref.StrategyRef)) {
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

func (r diagnosticRequest) authorize(snapshot service.ZapretCatalogSnapshot) error {
	view, err := makeCatalogView(snapshot)
	if err != nil {
		return err
	}
	if !view.Ready || view.Revision != r.ExpectedRevision {
		return errors.New("Zapret batch is unavailable or the strategy catalog changed")
	}
	for _, ref := range view.References {
		if ref.State == "applied" && ref.OutboundRef == r.OutboundRef && ref.StrategyRef == r.StrategyRef {
			return nil
		}
	}
	return errors.New("selected Zapret binding is no longer enabled")
}

func (c *catalogChange) selectedDraft() (strategy.Draft, error) {
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

func (c catalogChange) mutate(raw json.RawMessage, refs []outbounds.ZapretReference, draft strategy.Draft) (json.RawMessage, bool, error) {
	catalog, err := readCatalog(raw)
	if err != nil {
		return nil, false, err
	}
	if catalog.Revision() != c.ExpectedRevision {
		return nil, false, errors.New("Zapret strategy catalog changed; reload before saving")
	}
	previous, exists := catalog.Records[c.ID]
	if exists != (c.Action == "edit" || c.Action == "delete") {
		return nil, false, errors.New("Zapret strategy identity changed; reload before saving")
	}
	rebind := false
	if c.Action == "delete" {
		for _, ref := range refs {
			if ref.StrategyRef == c.ID {
				return nil, false, fmt.Errorf("strategy is referenced by Direct %q; clear or reassign its binding first", ref.OutboundRef)
			}
		}
		delete(catalog.Records, c.ID)
	} else {
		next, err := strategy.SealDraft(draft)
		if err != nil {
			return nil, false, err
		}
		catalog.Records[c.ID] = next
		for _, ref := range refs {
			if ref.Enabled && ref.StrategyRef == c.ID && next.Definition.Revision != previous.Definition.Revision {
				rebind = true
				break
			}
		}
	}
	if err := catalog.Validate(); err != nil {
		return nil, false, err
	}
	data, err := json.Marshal(catalog)
	return data, rebind, err
}
