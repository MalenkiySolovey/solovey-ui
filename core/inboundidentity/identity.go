// Package inboundidentity owns accepted inbound principal bindings and the
// admission lifetime of an inbound instance. It stores no live connections.
package inboundidentity

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	entitytypes "github.com/MalenkiySolovey/solovey-ui/internal/entities/types"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/sagernet/sing-box/adapter"
)

type Binding struct {
	Inbound   string
	Principal string
	ClientID  uint
}

// VPNUserID is the typed projection of the already proven local principal.
func (b Binding) VPNUserID() entitytypes.VPNUserID { return entitytypes.VPNUserID(b.ClientID) }

type principal struct {
	binding Binding
	token   string
}

// Epoch is specific to a constructed inbound, including hot replacements
// inside one Box. A surviving authenticated parent retains its original epoch.
type Epoch struct {
	mu         sync.RWMutex
	active     bool
	id         string
	tag        string
	ownerTag   string
	members    map[string]*Epoch
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
	epoch := &Epoch{id: id, tag: tag, ownerTag: tag, principals: map[string]*principal{}, tokens: map[string]*principal{}}
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

// PrepareInbound derives group membership from the actual decoded provider
// options, never from a caller's binding list. Each member keeps its own
// credential namespace and epoch; the group's admission lifetime retires them.
func (o *Owner) PrepareInbound(router adapter.Router, tag, kind string, options any, bindings []Binding) (adapter.Router, *Epoch, error) {
	o.mu.RLock()
	previous := o.inbounds[tag]
	conflict := previous != nil && previous.ownerTag != tag
	o.mu.RUnlock()
	if conflict {
		return nil, nil, errors.New("inbound identity belongs to a group")
	}
	tags, err := groupMemberTags(kind, options)
	if err != nil {
		return nil, nil, err
	}
	bound, root, err := o.Prepare(router, tag, bindings)
	if err != nil {
		return nil, nil, err
	}
	if len(tags) == 0 {
		return bound, root, nil
	}
	root.members = map[string]*Epoch{}
	for _, member := range tags {
		if member == "" || member == tag || root.members[member] != nil {
			return nil, nil, errors.New("ambiguous inbound group identity")
		}
		o.mu.RLock()
		previous := o.inbounds[member]
		conflict := previous != nil && previous.ownerTag != tag
		o.mu.RUnlock()
		if conflict {
			return nil, nil, errors.New("inbound group identity already owned")
		}
		_, epoch, err := o.Prepare(router, member, bindings)
		if err != nil {
			return nil, nil, err
		}
		epoch.ownerTag = tag
		root.members[member] = epoch
	}
	return bound, root, nil
}

// Publish follows successful manager construction. An unaccepted candidate
// cannot admit a flow. Replacement revokes the old parent admission lifetime.
func (o *Owner) Publish(tag string, epoch *Epoch) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if previous := o.inbounds[tag]; previous != nil {
		previous.revoke()
		for member := range previous.members {
			delete(o.inbounds, member)
		}
	}
	epoch.mu.Lock()
	epoch.active = true
	epoch.mu.Unlock()
	o.inbounds[tag] = epoch
	for tag, member := range epoch.members {
		member.mu.Lock()
		member.active = true
		member.mu.Unlock()
		o.inbounds[tag] = member
	}
}

func (e *Epoch) revoke() {
	e.mu.Lock()
	e.active = false
	e.principals = nil
	e.tokens = nil
	for _, member := range e.members {
		member.revoke()
	}
	e.mu.Unlock()
}

func (o *Owner) Revoke(tag string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if epoch := o.inbounds[tag]; epoch != nil {
		if root := o.inbounds[epoch.ownerTag]; root != nil {
			epoch = root
		}
		epoch.revoke()
		delete(o.inbounds, epoch.tag)
		for member := range epoch.members {
			delete(o.inbounds, member)
		}
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

// ResolvePrincipal binds verified protocol output to the accepted inbound
// epoch. The caller must hold the runtime mutation/lifecycle lease while pairing
// this projection with the current inbound's transport controller.
func (o *Owner) ResolvePrincipal(inbound, principal string) (Binding, string, bool) {
	o.mu.RLock()
	epoch := o.inbounds[inbound]
	o.mu.RUnlock()
	if epoch == nil {
		return Binding{}, "", false
	}
	epoch.mu.RLock()
	defer epoch.mu.RUnlock()
	entry := epoch.principals[principal]
	if !epoch.active || entry == nil {
		return Binding{}, "", false
	}
	return entry.binding, epoch.id, true
}

func (o *Owner) CurrentEpoch(inbound string) (string, bool) {
	o.mu.RLock()
	epoch := o.inbounds[inbound]
	o.mu.RUnlock()
	if epoch == nil {
		return "", false
	}
	epoch.mu.RLock()
	defer epoch.mu.RUnlock()
	return epoch.id, epoch.active
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
	if member := e.members[metadata.Inbound]; member != nil {
		return member.bind(ctx, metadata)
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
		return registry.QUICAdmission(ctx, accept)
	}
	proof.epoch.mu.RLock()
	defer proof.epoch.mu.RUnlock()
	if !proof.epoch.active || ctx.Err() != nil {
		return false
	}
	return registry.QUICAdmission(ctx, accept)
}

// AccountingUserID is read within Admission's accepted callback. The immutable
// stamp comes only from the authenticated inbound owner; a name alone never
// establishes an accounting identity. Admission owns the epoch lifetime lock.
func AccountingUserID(ctx context.Context, metadata adapter.InboundContext) entitytypes.VPNUserID {
	proof, _ := ctx.Value(stampKey{}).(*stamp)
	if proof == nil || proof.principal == nil || ctx.Err() != nil || metadata.Inbound != proof.epoch.tag || metadata.User != proof.principal.binding.Principal {
		return 0
	}
	return proof.principal.binding.VPNUserID()
}

// InventoryMetadata is applied after StatsTracker admission. Policy and route
// matching keep the authenticated principal; accounting separately uses its
// proven stable ID. Official inventory receives the opaque accepted binding.
func InventoryMetadata(ctx context.Context, metadata adapter.InboundContext) adapter.InboundContext {
	proof, _ := ctx.Value(stampKey{}).(*stamp)
	if proof != nil && proof.principal != nil && metadata.Inbound == proof.epoch.tag && metadata.User == proof.principal.binding.Principal {
		metadata.User = proof.principal.token
	}
	return metadata
}
