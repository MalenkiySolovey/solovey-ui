package assembly

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"runtime"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/sagernet/sing-tun"
	"gorm.io/gorm"
)

func InstalledZapretTUNRequest(db *gorm.DB) (*zapretactivation.TUNRequest, error) {
	inbounds, err := configuredInbounds(db)
	if err != nil {
		return nil, err
	}
	return installedZapretTUNRequest(inbounds, true)
}

func ValidateUnboundZapretTUN(db *gorm.DB) error {
	inbounds, err := configuredInbounds(db)
	if err != nil {
		return err
	}
	_, err = installedZapretTUNRequest(inbounds, false)
	return err
}

func configuredInbounds(db *gorm.DB) ([]json.RawMessage, error) {
	return optionalSection(db, &model.Inbound{}, func() ([]json.RawMessage, error) {
		return entityinbounds.GetAllConfig(db, entityinbounds.StoredUsers{})
	})
}

func installedZapretTUNRequest(inbounds []json.RawMessage, bound bool) (*zapretactivation.TUNRequest, error) {
	var result *zapretactivation.TUNRequest
	for _, raw := range inbounds {
		var kind struct {
			Type      string
			Interface string `json:"interface_name"`
		}
		if err := json.Unmarshal(raw, &kind); err != nil {
			return nil, err
		}
		if kind.Type != "tun" {
			continue
		}
		if !bound {
			if kind.Interface == zapretactivation.TUNInterface {
				return nil, fmt.Errorf("%w: remove the broker-managed TUN inbound before switching the last Zapret binding OFF", entityoutbounds.ErrZapretPacketPathUnqualified)
			}
			continue
		}
		denied := fmt.Errorf("%w: TUN cannot share this installed profile without one externally configured IPv4 gVisor inbound", entityoutbounds.ErrZapretPacketPathUnqualified)
		if result != nil || runtime.GOOS != "linux" || !tun.WithGVisor {
			return nil, denied
		}
		var input struct {
			Tag          string         `json:"tag"`
			Interface    string         `json:"interface_name"`
			Address      []netip.Prefix `json:"address"`
			MTU          uint32         `json:"mtu"`
			Stack        string         `json:"stack"`
			DNS          string         `json:"dns_mode"`
			AutoRoute    bool           `json:"auto_route"`
			AutoRedirect bool           `json:"auto_redirect"`
		}
		if json.Unmarshal(raw, &input) != nil || input.Interface != zapretactivation.TUNInterface || len(input.Address) != 1 ||
			input.Stack != "gvisor" || input.DNS != tun.DNSModeDisabled || input.AutoRoute || input.AutoRedirect {
			return nil, denied
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(raw, &keys); err != nil {
			return nil, err
		}
		for key := range keys {
			switch key {
			case "type", "tag", "interface_name", "address", "mtu", "stack", "dns_mode", "auto_route", "auto_redirect":
			default:
				return nil, denied
			}
		}
		request := zapretactivation.TUNRequest{Tag: input.Tag, Address: input.Address[0], MTU: input.MTU}
		if !request.Valid() {
			return nil, denied
		}
		result = &request
	}
	return result, nil
}
