//go:build !minimal

package zapret

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	Queue           uint16 `json:"queue,omitempty"`
	State           string `json:"state"`
}

type catalogChange struct {
	Action           string              `json:"action"`
	ExpectedRevision string              `json:"expectedRevision"`
	ID               string              `json:"id,omitempty"`
	Draft            *strategy.Draft     `json:"draft,omitempty"`
	Transfer         *strategy.Transfer  `json:"transfer,omitempty"`
	List             *strategy.NamedList `json:"list,omitempty"`
	Blob             *string             `json:"blob,omitempty"`
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
				status.Queue = observation.Queue
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
		return errors.New("the Zapret batch is unavailable or the strategy catalog changed")
	}
	for _, ref := range view.References {
		if ref.State == "applied" && ref.OutboundRef == r.OutboundRef && ref.StrategyRef == r.StrategyRef {
			return nil
		}
	}
	return errors.New("selected Zapret binding is no longer enabled")
}

func (c *catalogChange) prepare() error {
	if len(c.ExpectedRevision) != 64 {
		return errors.New("missing Zapret catalog revision")
	}
	if c.Action == "create-list" || c.Action == "edit-list" || c.Action == "delete-list" {
		if c.ID != "" && c.Draft == nil && c.Transfer == nil && c.Blob == nil && (c.List != nil) == (c.Action != "delete-list") {
			return nil // The strategy catalog validates the list ID and content.
		}
		return errors.New("invalid Zapret native list change")
	}
	if c.Action == "create-blob" || c.Action == "edit-blob" || c.Action == "delete-blob" {
		if c.ID != "" && c.Draft == nil && c.Transfer == nil && c.List == nil && (c.Blob != nil) == (c.Action != "delete-blob") {
			return nil // The strategy catalog validates the asset ID and data.
		}
		return errors.New("invalid Zapret native blob change")
	}
	if c.List != nil || c.Blob != nil || c.ID != "" && !zapretactivation.ManagedStrategyRef(c.ID) {
		return errors.New("invalid Zapret strategy change")
	}
	allocate := false
	switch c.Action {
	case "delete":
		if c.ID != "" && c.Draft == nil && c.Transfer == nil {
			return nil
		}
	case "edit", "create":
		if c.Draft != nil && c.Transfer == nil && c.Draft.ID == "" && (c.Action == "edit") == (c.ID != "") {
			if c.Action == "edit" {
				return nil
			}
			allocate = true
		}
	case "import":
		allocate = c.ID == "" && c.Draft == nil && c.Transfer != nil
	}
	if !allocate {
		return errors.New("invalid Zapret strategy change")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	c.ID = zapretactivation.ManagedStrategyPrefix + hex.EncodeToString(id[:])
	return nil
}

func (c catalogChange) mutate(raw json.RawMessage, refs []outbounds.ZapretReference) (json.RawMessage, bool, error) {
	previous, err := readCatalog(raw)
	if err != nil {
		return nil, false, err
	}
	if previous.Revision() != c.ExpectedRevision {
		return nil, false, errors.New("the Zapret strategy catalog changed; reload before saving")
	}
	next := previous
	switch c.Action {
	case "create-list", "edit-list", "delete-list":
		if _, exists := previous.Lists[c.ID]; exists != (c.Action != "create-list") {
			return nil, false, errors.New("the Zapret native list ID is already in use or does not exist")
		}
		next, err = previous.WithNamedList(c.ID, c.List)
	case "create-blob", "edit-blob", "delete-blob":
		if _, exists := previous.BlobAssets[c.ID]; exists != (c.Action != "create-blob") {
			return nil, false, errors.New("the Zapret native blob ID is already in use or does not exist")
		}
		next, err = previous.WithNamedBlob(c.ID, c.Blob)
	case "import":
		next, err = previous.WithTransfer(c.ID, *c.Transfer)
	case "delete", "edit", "create":
		_, exists := previous.Records[c.ID]
		if exists != (c.Action != "create") {
			return nil, false, errors.New("the Zapret strategy identity changed; reload before saving")
		}
		next.Records = maps.Clone(previous.Records)
		if c.Action == "delete" {
			for _, ref := range refs {
				if ref.StrategyRef == c.ID {
					return nil, false, fmt.Errorf("strategy is referenced by Direct %q; clear or reassign its binding first", ref.OutboundRef)
				}
			}
			delete(next.Records, c.ID)
		} else {
			draft := *c.Draft
			draft.ID = c.ID
			next.Records[c.ID], err = previous.SealDraft(draft)
		}
	default:
		return nil, false, errors.New("invalid Zapret catalog change")
	}
	if err != nil {
		return nil, false, err
	}
	if err := next.Validate(); err != nil {
		return nil, false, err
	}
	rebind := slices.ContainsFunc(refs, func(ref outbounds.ZapretReference) bool {
		return ref.Enabled && previous.Records[ref.StrategyRef].Definition.Revision != next.Records[ref.StrategyRef].Definition.Revision
	})
	data, err := json.Marshal(next)
	return data, rebind, err
}
