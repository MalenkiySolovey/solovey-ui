package outbounds

import (
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	hostoutbounds "github.com/MalenkiySolovey/solovey-host/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
)

const zapretOption = "zapret"

var (
	ErrZapretPacketPathUnqualified = errors.New("ZAPRET_PACKET_PATH_UNQUALIFIED: disable the binding before activating this outbound")
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

// projectZapretOutbound is used before any candidate/live write. Returning an
// error for ON is intentional: omitting A or passing its unmarked Direct to
// sing-box could select a different outbound and silently change user intent.
func projectZapretOutbound(row model.Outbound) (model.Outbound, error) {
	return projectZapretOptions(row, false)
}

func projectZapretOptions(row model.Outbound, heldRestoreValidation bool) (model.Outbound, error) {
	fields, binding, err := zapretOptions(row)
	if err != nil || binding == nil {
		return row, err
	}
	if binding.Enabled && !heldRestoreValidation {
		return model.Outbound{}, ErrZapretPacketPathUnqualified
	}
	delete(fields, zapretOption)
	row.Options, err = json.Marshal(fields)
	return row, err
}
