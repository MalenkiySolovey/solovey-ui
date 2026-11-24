package outbounds

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	hostoutbounds "github.com/MalenkiySolovey/solovey-host/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"gorm.io/gorm"
)

const zapretOption = "zapret"

var (
	ErrZapretPacketPathUnqualified = errors.New("ZAPRET_PACKET_PATH_UNQUALIFIED: a ready installed Zapret profile is required for this outbound")
	errZapretBindingInvalid        = errors.New("ZAPRET_BINDING_INVALID: expected a Direct outbound binding")
	errZapretChangeRequired        = errors.New("ZAPRET_BINDING_EDIT_REQUIRED: include the binding explicitly when editing the outbound")
)

// zapretOptions reads the attachment retained in the Outbound's desired options.
// It never needs the optional component, including when validating a backup.
func zapretOptions(row model.Outbound) (map[string]json.RawMessage, *hostoutbounds.ZapretBinding, error) {
	if len(row.Options) == 0 {
		return nil, nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(row.Options, &fields); err != nil {
		return nil, nil, err
	}
	for key := range fields {
		if strings.EqualFold(key, "protect_path") {
			return nil, nil, errors.New("protect_path is reserved for installed Core socket admission")
		}
	}
	raw, present := fields[zapretOption]
	if !present {
		return nil, nil, nil
	}
	if len(raw) > 1024 || jsonvalue.StrictObject(row.Options) != nil {
		return nil, nil, errZapretBindingInvalid
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil || keys == nil {
		return nil, nil, errZapretBindingInvalid
	}
	for key := range keys {
		if key != "outboundRef" && key != "enabled" && key != "strategyRef" {
			return nil, nil, errZapretBindingInvalid
		}
	}
	var binding hostoutbounds.ZapretBinding
	var enabled *bool
	var detour string
	if json.Unmarshal(raw, &binding) != nil || json.Unmarshal(keys["enabled"], &enabled) != nil || enabled == nil ||
		(fields["detour"] != nil && json.Unmarshal(fields["detour"], &detour) != nil) {
		return nil, nil, errZapretBindingInvalid
	}
	if err := binding.Validate(row.Tag, row.Type, detour); err != nil {
		return nil, nil, err
	}
	return fields, &binding, nil
}

func requireExplicitZapretChange(previous, candidate model.Outbound) error {
	_, binding, err := zapretOptions(previous)
	if err != nil || binding == nil {
		return err
	}
	_, incoming, err := zapretOptions(candidate)
	if err != nil {
		return err
	}
	if incoming == nil {
		return errZapretChangeRequired
	}
	return nil
}

func zapretLifecycleChange(allowed bool, rows ...model.Outbound) (*singboxapply.Change, error) {
	for _, row := range rows {
		_, binding, err := zapretOptions(row)
		if err != nil {
			return nil, err
		}
		if binding != nil && binding.Enabled {
			if !allowed {
				return nil, ErrZapretPacketPathUnqualified
			}
			return &singboxapply.Change{NeedsRestart: true, QuiesceCore: true, RestartReason: "Zapret binding changed"}, nil
		}
	}
	return nil, nil
}

// CheckZapretStartup validates desired intent without reserving a broker
// generation. It is also used by the component disable dependency check.
func CheckZapretStartup(db *gorm.DB, ready bool) (bool, error) {
	if !db.Migrator().HasTable(&model.Outbound{}) {
		return false, nil
	}
	var rows []model.Outbound
	if err := db.Find(&rows).Error; err != nil {
		return false, err
	}
	found := false
	for _, row := range rows {
		fields, binding, err := zapretOptions(row)
		if err != nil {
			return false, err
		}
		if binding == nil || !binding.Enabled {
			continue
		}
		if !ready || row.RemoteMissing {
			return false, fmt.Errorf("outbound %q: %w", binding.OutboundRef, ErrZapretPacketPathUnqualified)
		}
		if _, err := zapretDialBinding(fields); err != nil {
			return false, fmt.Errorf("outbound %q: %w", binding.OutboundRef, err)
		}
		found = true
	}
	return found, nil
}

func zapretDialBinding(fields map[string]json.RawMessage) (zapretactivation.DialBinding, error) {
	var dial zapretactivation.DialBinding
	for key, raw := range fields {
		field := strings.ToLower(key)
		switch field {
		case "protect_path", "routing_mark", "netns":
			return dial, fmt.Errorf("%w: selected Dial field %s has no installed admission", ErrZapretPacketPathUnqualified, field)
		case "bind_interface", "inet4_bind_address", "inet6_bind_address":
			var entered *string
			if key != field || json.Unmarshal(raw, &entered) != nil {
				return dial, fmt.Errorf("%w: invalid Dial field %s", ErrZapretPacketPathUnqualified, field)
			}
			if entered == nil {
				continue // The native option is unset; preserve the original JSON.
			}
			if field == "bind_interface" {
				dial.Interface = *entered
				continue
			}
			addr, err := netip.ParseAddr(*entered)
			if err != nil || field == "inet4_bind_address" && !addr.Is4() ||
				field == "inet6_bind_address" && (!addr.Is6() || addr.Is4In6() || addr.Zone() != "") {
				return dial, fmt.Errorf("%w: invalid Dial field %s", ErrZapretPacketPathUnqualified, field)
			}
			if !addr.IsUnspecified() {
				if addr.Is4() {
					dial.IPv4 = addr
				} else {
					dial.IPv6 = addr
				}
			}
		}
	}
	if !dial.Valid() {
		return dial, fmt.Errorf("%w: use a valid Linux interface and unicast source addresses", ErrZapretPacketPathUnqualified)
	}
	return dial, nil
}

// projectZapretOutbound is used outside authorized Core startup. Returning an
// error for ON is intentional: omitting A or passing its unmarked Direct to
// sing-box could select a different outbound and silently change user intent.
func projectZapretOutbound(row model.Outbound) (model.Outbound, error) {
	return projectZapretOptions(row, false)
}

func projectZapretOptions(row model.Outbound, heldRestoreValidation bool) (model.Outbound, error) {
	return projectZapretRuntime(row, heldRestoreValidation, nil)
}

// The installed broker admits the complete set in one operation. Projection
// reads the resulting grants; offline and hot reload do not receive them.
type ZapretBatchAdmitter func(zapretactivation.Validation) ([]zapretactivation.Grant, error)

type ZapretAdmitter func(zapretactivation.Request) (zapretactivation.Grant, error)

func zapretRequest(row model.Outbound) (zapretactivation.Request, bool, error) {
	fields, binding, err := zapretOptions(row)
	if err != nil || binding == nil || !binding.Enabled {
		return zapretactivation.Request{}, false, err
	}
	if row.RemoteMissing {
		return zapretactivation.Request{}, false, fmt.Errorf("outbound %q: %w", binding.OutboundRef, ErrZapretPacketPathUnqualified)
	}
	dial, err := zapretDialBinding(fields)
	if err != nil {
		return zapretactivation.Request{}, false, fmt.Errorf("outbound %q: %w", binding.OutboundRef, err)
	}
	serialized, err := json.Marshal(row)
	if err != nil {
		return zapretactivation.Request{}, false, err
	}
	return zapretactivation.Request{OutboundRef: binding.OutboundRef, OutboundRevision: fmt.Sprintf("%x", sha256.Sum256(serialized)), StrategyRef: binding.StrategyRef, Dial: dial}, true, nil
}

func ZapretRequests(db *gorm.DB) ([]zapretactivation.Request, error) {
	var rows []*model.Outbound
	if err := db.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return zapretRequests(rows)
}

func zapretRequests(rows []*model.Outbound) ([]zapretactivation.Request, error) {
	var requests []zapretactivation.Request
	for _, row := range rows {
		request, enabled, err := zapretRequest(*row)
		if err != nil {
			return nil, err
		}
		if enabled {
			requests = append(requests, request)
		}
	}
	return requests, nil
}

func admitZapretRows(db *gorm.DB, rows []*model.Outbound, admit ZapretBatchAdmitter) (ZapretAdmitter, error) {
	requests, err := zapretRequests(rows)
	if err != nil || len(requests) == 0 {
		return nil, err
	}
	input, err := ZapretValidation(db, requests)
	if err != nil {
		return nil, err
	}
	grants, err := admit(input)
	if err != nil {
		refs := make([]string, len(requests))
		for i, request := range requests {
			refs[i] = fmt.Sprintf("outbound %q", request.OutboundRef)
		}
		return nil, fmt.Errorf("%s: %w", strings.Join(refs, ", "), err)
	}
	if len(grants) != len(requests) {
		return nil, ErrZapretPacketPathUnqualified
	}
	byRequest := make(map[zapretactivation.Request]zapretactivation.Grant, len(requests))
	generations := make(map[string]bool, len(grants))
	for i, grant := range grants {
		if _, ok := grant.ProtectPath(requests[i]); !ok || generations[grant.Generation] {
			return nil, ErrZapretPacketPathUnqualified
		}
		if raw := input.Definitions[requests[i].StrategyRef]; raw != nil {
			var definition struct{ Revision string }
			if json.Unmarshal(raw, &definition) != nil || definition.Revision != grant.StrategyRevision {
				return nil, ErrZapretPacketPathUnqualified
			}
		}
		generations[grant.Generation] = true
		byRequest[requests[i]] = grant
	}
	return func(request zapretactivation.Request) (zapretactivation.Grant, error) {
		grant, ok := byRequest[request]
		if !ok {
			return zapretactivation.Grant{}, ErrZapretPacketPathUnqualified
		}
		return grant, nil
	}, nil
}

func projectZapretRuntime(row model.Outbound, heldRestoreValidation bool, admit ZapretAdmitter) (model.Outbound, error) {
	fields, binding, err := zapretOptions(row)
	if err != nil || binding == nil {
		return row, err
	}
	if binding.Enabled && !heldRestoreValidation {
		fail := func(cause error) (model.Outbound, error) {
			return model.Outbound{}, fmt.Errorf("outbound %q: %w", binding.OutboundRef, cause)
		}
		// Only a fresh socket in the normal namespace can enter the broker.
		if admit == nil {
			return fail(ErrZapretPacketPathUnqualified)
		}
		request, enabled, err := zapretRequest(row)
		if err != nil || !enabled {
			return model.Outbound{}, errors.Join(ErrZapretPacketPathUnqualified, err)
		}
		grant, err := admit(request)
		if err != nil {
			return fail(err)
		}
		path, ok := grant.ProtectPath(request)
		if !ok {
			return fail(ErrZapretPacketPathUnqualified)
		}
		fields["protect_path"], _ = json.Marshal(path)
	}
	delete(fields, zapretOption)
	row.Options, err = json.Marshal(fields)
	return row, err
}
