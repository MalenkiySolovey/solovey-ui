package entityinbounds

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	entityidentity "github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/composition"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	entityprotocol "github.com/MalenkiySolovey/solovey-ui/internal/entities/protocol"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveeligibility"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/tagrefs"
	suburi "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

func Save(req SaveRequest) (*singboxapply.Change, error) {
	action, ok := ParseAction(req.Action)
	if !ok {
		return nil, common.NewErrorf("unknown action: %s", req.Action)
	}
	switch action {
	case ActionNew:
		return saveNew(req)
	case ActionEdit:
		return saveEdited(req)
	case ActionDel:
		return saveDeleted(req)
	default:
		return nil, common.NewErrorf("unknown action: %s", req.Action)
	}
}
func ParseAction(action string) (SaveAction, bool) {
	saveAction := SaveAction(action)
	for _, supported := range supportedSaveActions {
		if saveAction == supported {
			return saveAction, true
		}
	}
	return "", false
}
func SupportedActionStrings() []string {
	actions := make([]string, 0, len(supportedSaveActions))
	for _, action := range supportedSaveActions {
		actions = append(actions, string(action))
	}
	return actions
}
func saveNew(req SaveRequest) (*singboxapply.Change, error) {
	inbound, err := DecodeForSave(req.Tx, req.Data)
	if err != nil {
		return nil, err
	}
	if err := saveeligibility.Validate(req.Tx, string(ActionNew), inbound.Id, &model.Inbound{}, "inbounds", inbound.Type); err != nil {
		return nil, err
	}
	if err := FillAndSave(req.Tx, &inbound, req.Hostname); err != nil {
		return nil, err
	}
	if req.ClientHooks != nil {
		if err := req.ClientHooks.UpdateClientsOnInboundAdd(req.Tx, req.InitUserIDs, inbound.Id, req.Hostname); err != nil {
			return nil, err
		}
	}
	return CoreChangeForSavedRow(req.Tx, inbound.Id, inbound.Tag, "")
}
func saveEdited(req SaveRequest) (*singboxapply.Change, error) {
	inbound, err := DecodeForSave(req.Tx, req.Data)
	if err != nil {
		return nil, err
	}
	if err := saveeligibility.Validate(req.Tx, string(ActionEdit), inbound.Id, &model.Inbound{}, "inbounds", inbound.Type); err != nil {
		return nil, err
	}
	oldTag, err := TagByID(req.Tx, inbound.Id)
	if err != nil {
		return nil, err
	}
	if oldTag != "" && oldTag != inbound.Tag {
		refs, err := tagrefs.Inbound(req.Tx, oldTag)
		if err != nil {
			return nil, err
		}
		if len(refs) > 0 {
			return nil, tagrefs.FormatError("inbound", oldTag, refs)
		}
	}
	if err := FillAndSave(req.Tx, &inbound, req.Hostname); err != nil {
		return nil, err
	}
	if req.ClientHooks != nil {
		if err := req.ClientHooks.UpdateLinksByInboundChange(req.Tx, &[]model.Inbound{inbound}, req.Hostname, oldTag); err != nil {
			return nil, err
		}
	}
	return CoreChangeForSavedRow(req.Tx, inbound.Id, inbound.Tag, oldTag)
}
func DecodeForSave(tx *gorm.DB, data json.RawMessage) (model.Inbound, error) {
	var inbound model.Inbound
	if err := inbound.UnmarshalJSON(data); err != nil {
		return inbound, err
	}
	if managedNativeInbound(inbound.Type) || composition.IsGroup(inbound.Type) {
		if jsonvalue.StrictObject(data) != nil {
			return inbound, common.NewError("NATIVE_INBOUND_INPUT_INVALID")
		}
		var submitted map[string]json.RawMessage
		_ = json.Unmarshal(data, &submitted)
		if inbound.Type == "call" {
			for _, field := range []string{"listen", "listen_port"} {
				if _, supplied := submitted[field]; supplied {
					return inbound, common.NewError("CALL_INBOUND_NO_LISTENER: Call uses a platform room")
				}
			}
			if inbound.TlsId != 0 {
				return inbound, common.NewError("CALL_INBOUND_NO_TLS_TEMPLATE: platform TLS belongs to the Call transport")
			}
		}
		for _, field := range []string{"users", "tls"} {
			if raw, present := submitted[field]; present && string(raw) != "null" {
				return inbound, common.NewError("NATIVE_INBOUND_OWNED_FIELD: use the existing Users assignment and TLS template owners")
			}
		}
		if inbound.Type == "ssh" {
			if raw, present := submitted["fallback"]; present && string(raw) != "null" {
				return inbound, common.NewError("NATIVE_INBOUND_OWNED_FIELD: SSH authentication belongs to the existing Users owner")
			}
		}
	}
	if err := entityidentity.ValidateTypeTag(inbound.Type, inbound.Tag); err != nil {
		return inbound, err
	}
	if err := validateJSONShape(inbound); err != nil {
		return inbound, err
	}
	if composition.IsGroup(inbound.Type) && inbound.TlsId != 0 {
		return inbound, common.NewError("INBOUND_GROUP_OWNED_FIELD: select TLS on the canonical member inbound")
	}
	if err := diagnostics.FirstError(LegacyOptionsFindings(inbound.Type, "inbound.options", inbound.Options)); err != nil {
		return inbound, err
	}
	prepared, err := entityprotocol.PrepareUpgrade(inbound.Type, "inbound", "inbound.options", inbound.Options)
	if err != nil {
		return inbound, err
	}
	if err := diagnostics.FirstError(entityprotocol.CurrentFindings(inbound.Type, "inbound", "inbound.options", prepared.Candidate)); err != nil {
		return inbound, err
	}
	inbound.Options = prepared.Candidate
	if err := diagnostics.FirstError(TUNDNSFindings(append(append([]byte(`{"inbounds":[`), data...), []byte(`]}`)...))); err != nil {
		return inbound, err
	}
	if inbound.TlsId > 0 {
		if err := tx.Model(model.Tls{}).Where("id = ?", inbound.TlsId).Find(&inbound.Tls).Error; err != nil {
			return inbound, err
		}
	}
	if inbound.Type == "trusttunnel" && entitycapabilities.Resolve("inbounds", inbound.Type).Available {
		var tls struct {
			Enabled bool `json:"enabled"`
		}
		if inbound.TlsId == 0 || inbound.Tls == nil || json.Unmarshal(inbound.Tls.Server, &tls) != nil || !tls.Enabled {
			return inbound, common.NewError("TRUSTTUNNEL_TLS_REQUIRED: select an enabled TLS template")
		}
	}
	return inbound, nil
}

func preserveUnavailableNativeInbound(tx *gorm.DB, inbound *model.Inbound) error {
	if (!managedNativeInbound(inbound.Type) && !composition.IsGroup(inbound.Type)) || entitycapabilities.Resolve("inbounds", inbound.Type).Available {
		return nil
	}
	var stored model.Inbound
	if err := tx.Where("id = ?", inbound.Id).First(&stored).Error; err != nil {
		return err
	}
	if inbound.TlsId != stored.TlsId || !sameNativeJSON(inbound.Options, stored.Options) ||
		!sameNativeJSON(inbound.Addrs, stored.Addrs) || !sameNativeJSON(inbound.OutJson, stored.OutJson) {
		return common.NewError("NATIVE_INBOUND_CAPABILITY_UNAVAILABLE: preserve stored protocol fields until their capability is available")
	}
	// Keep the captured owner fields byte-for-byte, including future options.
	inbound.Options, inbound.Addrs, inbound.OutJson = stored.Options, stored.Addrs, stored.OutJson
	return nil
}

func sameNativeJSON(left, right json.RawMessage) bool {
	decode := func(raw json.RawMessage) (any, bool) {
		if len(bytes.TrimSpace(raw)) == 0 {
			return nil, true
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) != nil {
			return nil, false
		}
		return value, decoder.Decode(new(any)) == io.EOF
	}
	a, aOK := decode(left)
	b, bOK := decode(right)
	if !aOK || !bOK {
		return false
	}
	return reflect.DeepEqual(a, b)
}

func validateJSONShape(inbound model.Inbound) error {
	if err := jsonvalue.OptionalObject("inbound options", inbound.Options); err != nil {
		return err
	}
	if err := jsonvalue.OptionalArray("inbound addresses", inbound.Addrs); err != nil {
		return err
	}
	if len(inbound.Addrs) > 0 {
		if err := suburi.ValidateAddresses(inbound.Addrs); err != nil {
			return err
		}
	}
	return jsonvalue.OptionalObject("inbound outbound config", inbound.OutJson)
}

func ValidateStored(db *gorm.DB) error {
	if db == nil {
		return common.NewError("inbound persistence is unavailable")
	}
	if !db.Migrator().HasTable(&model.Inbound{}) {
		return nil
	}
	var rows []model.Inbound
	if err := db.Order("id").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := validateJSONShape(row); err != nil {
			return common.NewErrorf("stored inbound row %d: %v", row.Id, err)
		}
	}
	_, err := composition.Build(rows)
	return err
}
func FillAndSave(tx *gorm.DB, inbound *model.Inbound, hostname string) error {
	if err := validateInboundGroupCandidate(tx, *inbound); err != nil {
		return err
	}
	if err := prepareInboundCredentials(tx, inbound); err != nil {
		return err
	}
	if inbound.Id != 0 {
		if err := preserveUnavailableNativeInbound(tx, inbound); err != nil {
			return err
		}
	}
	if err := FillOutboundJSONFromDB(tx, inbound, hostname); err != nil {
		return err
	}
	sortOrder, err := entityorder.ForSave(tx, &model.Inbound{}, inbound.Id)
	if err != nil {
		return err
	}
	inbound.SortOrder = sortOrder
	return tx.Save(inbound).Error
}
func TagByID(tx *gorm.DB, id uint) (string, error) {
	var tag string
	err := tx.Model(model.Inbound{}).Select("tag").Where("id = ?", id).Find(&tag).Error
	return tag, err
}
func saveDeleted(req SaveRequest) (*singboxapply.Change, error) {
	var tag string
	if err := json.Unmarshal(req.Data, &tag); err != nil {
		return nil, err
	}
	if err := entityidentity.ValidateTag(tag); err != nil {
		return nil, err
	}
	refs, err := tagrefs.Inbound(req.Tx, tag)
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 {
		return nil, tagrefs.FormatError("inbound", tag, refs)
	}
	id, err := IDByTag(req.Tx, tag)
	if err != nil {
		return nil, err
	}
	var previous model.Inbound
	if err := req.Tx.Select("id", "type", "options").Where("id = ?", id).Find(&previous).Error; err != nil {
		return nil, err
	}
	if err := validateInboundGroupLeases(req.Tx, previous, model.Inbound{}); err != nil {
		return nil, err
	}
	if req.ClientHooks != nil {
		if err := req.ClientHooks.UpdateClientsOnInboundDelete(req.Tx, id, tag); err != nil {
			return nil, err
		}
	}
	if err := req.Tx.Where("tag = ?", tag).Delete(model.Inbound{}).Error; err != nil {
		return nil, err
	}
	change := &singboxapply.Change{RemoveTags: []string{tag}}
	// Removing a group restores its canonical members as independent inbounds.
	// The existing complete-candidate restart owns that structural transition.
	if composition.IsGroup(previous.Type) {
		change.NeedsRestart, change.RestartReason = true, "inbound group membership changed"
	}
	return change, nil
}
func IDByTag(tx *gorm.DB, tag string) (uint, error) {
	var id uint
	err := tx.Model(model.Inbound{}).Select("id").Where("tag = ?", tag).Scan(&id).Error
	return id, err
}
func CoreChangeForSavedRow(tx *gorm.DB, id uint, tag string, oldTag string) (*singboxapply.Change, error) {
	change := &singboxapply.Change{ReloadIDs: []uint{id}}
	var row model.Inbound
	if err := tx.Select("id", "type").First(&row, id).Error; err != nil {
		return nil, err
	}
	if composition.IsGroup(row.Type) {
		change.NeedsRestart, change.RestartReason = true, "inbound group membership changed"
	}
	if oldTag != "" && oldTag != tag {
		change.RemoveTags = []string{oldTag}
	}
	serviceIDs, err := tagrefs.SSMCascadeServiceIDs(tx, tag)
	if err != nil {
		return nil, err
	}
	change.CascadeServiceIDs = serviceIDs
	return change, nil
}
