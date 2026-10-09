// Package inboundidentity owns accepted inbound principal bindings and the
// admission lifetime of an inbound instance. It stores no live connections.
package inboundidentity

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/sagernet/sing-box/adapter"
)

type Binding struct {
	Inbound   string
	Principal string
	ClientID  uint
}

type principal struct {
	binding Binding
	token   string
}

// Epoch is specific to a constructed inbound, including hot replacements
// inside one Box. A surviving authenticated parent retains its original epoch.
type Epoch struct {
	mu         sync.RWMutex
	active     bool
	tag        string
	principals map[string]*principal
	tokens     map[string]*principal
}

type Owner struct {
	mu       sync.RWMutex
	inbounds map[string]*Epoch
}

func NewOwner() *Owner { return &Owner{inbounds: map[string]*Epoch{}} }

func (o *Owner) Prepare(router adapter.Router, tag string, bindings []Binding) (adapter.Router, *Epoch, error) {
	id, err := common.RandomUUID()
	if err != nil {
		return nil, nil, errors.New("inbound identity generation failed")
	}
	epoch := &Epoch{tag: tag, principals: map[string]*principal{}, tokens: map[string]*principal{}}
	for _, binding := range bindings {
		if binding.Inbound != tag || binding.Principal == "" || binding.ClientID == 0 {
			continue
		}
		if _, duplicate := epoch.principals[binding.Principal]; duplicate {
			return nil, nil, errors.New("ambiguous authenticated inbound principal")
		}
		entry := &principal{binding: binding, token: fmt.Sprintf("__solovey_principal/%s/%d", id, len(epoch.principals))}
		epoch.principals[binding.Principal] = entry
		epoch.tokens[entry.token] = entry
	}
	return &boundRouter{Router: router, epoch: epoch}, epoch, nil
}

// Publish follows successful manager construction. An unaccepted candidate
// cannot admit a flow. Replacement revokes the old parent admission lifetime.
func (o *Owner) Publish(tag string, epoch *Epoch) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if previous := o.inbounds[tag]; previous != nil {
		previous.revoke()
	}
	epoch.mu.Lock()
	epoch.active = true
	epoch.mu.Unlock()
	o.inbounds[tag] = epoch
}

func (e *Epoch) revoke() {
	e.mu.Lock()
	e.active = false
	e.principals = nil
	e.tokens = nil
	e.mu.Unlock()
}

func (o *Owner) Revoke(tag string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if epoch := o.inbounds[tag]; epoch != nil {
		epoch.revoke()
		delete(o.inbounds, tag)
	}
}

func (o *Owner) Close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for tag, epoch := range o.inbounds {
		epoch.revoke()
		delete(o.inbounds, tag)
	}
}

func (o *Owner) Resolve(inbound, token string) (Binding, bool) {
	o.mu.RLock()
	epoch := o.inbounds[inbound]
	o.mu.RUnlock()
	if epoch == nil {
		return Binding{}, false
	}
	epoch.mu.RLock()
	defer epoch.mu.RUnlock()
	entry, ok := epoch.tokens[token]
	if !epoch.active || !ok {
		return Binding{}, false
	}
	return entry.binding, true
}

type stampKey struct{}
type stamp struct {
	epoch     *Epoch
	principal *principal
}

func (e *Epoch) bind(ctx context.Context, metadata adapter.InboundContext) (context.Context, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.active {
		return nil, errors.New("inbound_generation_retired")
	}
	// The pinned Snell constructor falls back to PSK-only when users is empty.
	// Product-managed credentials must never gain that anonymous fallback when
	// the last client is disabled. Require the authenticated runtime principal;
	// a nonempty but unproven binding still remains unknown in the inventory.
	if registry.Resolve("inbounds", metadata.InboundType).AuthenticatedUsers && (metadata.Inbound != e.tag || metadata.User == "") {
		return nil, errors.New("authenticated_inbound_user_required")
	}
	var entry *principal
	if metadata.Inbound == e.tag {
		entry = e.principals[metadata.User]
	}
	return context.WithValue(ctx, stampKey{}, &stamp{epoch: e, principal: entry}), nil
}

// Admission holds the inbound lifetime through policy and official inventory
// publication. Revoke waits for admitted callbacks and fences subsequent ones.
func Admission(ctx context.Context, accept func()) bool {
	proof, _ := ctx.Value(stampKey{}).(*stamp)
	if proof == nil {
		accept()
		return true
	}
	proof.epoch.mu.RLock()
	defer proof.epoch.mu.RUnlock()
	if !proof.epoch.active {
		return false
	}
	accept()
	return true
}

// InventoryMetadata is applied after StatsTracker admission. Policy, counters
// and route matching keep the original authenticated principal. Only the
// official inventory receives the opaque proven client binding.
func InventoryMetadata(ctx context.Context, metadata adapter.InboundContext) adapter.InboundContext {
	proof, _ := ctx.Value(stampKey{}).(*stamp)
	if proof != nil && proof.principal != nil && metadata.Inbound == proof.epoch.tag && metadata.User == proof.principal.binding.Principal {
		metadata.User = proof.principal.token
	}
	return metadata
}
