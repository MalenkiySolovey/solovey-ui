package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/assembly"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"gorm.io/gorm"
)

func (r *Runtime) installedZapretAdmitter(ctx context.Context, admitted **zapretactivation.Grant) outbounds.ZapretAdmitter {
	return func(target zapretactivation.Request) (zapretactivation.Grant, error) {
		if *admitted != nil || !r.zapretComponentStarted() {
			return zapretactivation.Grant{}, outbounds.ErrZapretPacketPathUnqualified
		}
		var grant zapretactivation.Grant
		client := broker.NewClient(broker.RolePanel)
		if _, err := client.Invoke(ctx, broker.Call{Verb: broker.VerbZapretAcquire, OperationID: "zapret-core-start", Payload: target, Timeout: 10 * time.Second}, &grant); err != nil {
			return zapretactivation.Grant{}, errors.Join(outbounds.ErrZapretPacketPathUnqualified, err)
		}
		*admitted = &grant
		return grant, nil
	}
}

// The Core lifecycle lock also serializes the receipt. Keep it until the
// broker confirms retirement so an OFF save cannot report cleanup prematurely.
func (r *Runtime) retireZapretGrant() error {
	if r.zapretGrant == nil {
		return nil
	}
	client := broker.NewClient(broker.RolePanel)
	_, err := client.Invoke(context.Background(), broker.Call{Verb: broker.VerbZapretRelease, OperationID: "zapret-core-stop", Payload: r.zapretGrant, Timeout: 5 * time.Second}, nil)
	if err != nil {
		return fmt.Errorf("Zapret generation retirement was not confirmed: %w", err)
	}
	r.zapretGrant = nil
	return nil
}

func (s *ConfigService) validateCoreSaveCandidate(tx *gorm.DB, obj string, plan configSavePlan) error {
	switch obj {
	case "inbounds", "outbounds", "endpoints", "config":
		bound, err := outbounds.CheckZapretStartup(tx, true)
		if err != nil {
			return err
		}
		if bound || plan.RequiresCoreQuiescence() {
			ready := s.runtime().zapretComponentStarted()
			var setting model.Setting
			if err := tx.Where("key = ?", enabledstate.SettingKey("zapret")).Limit(1).Find(&setting).Error; err != nil {
				return err
			}
			if setting.Key != "" {
				enabled, err := strconv.ParseBool(setting.Value)
				ready = ready && err == nil && enabled
			}
			return assembly.ValidateCoreRestartFromDB(tx, ready)
		}
	}
	return s.validateNativeInboundCandidate(tx)
}

// ComponentHost cannot remove a dependency while an enabled outbound still
// owns it. Explicitly switching that binding OFF retires its grant first.
func validateZapretDisable(tx *gorm.DB, data json.RawMessage) error {
	settings, err := settingsDecodeSaveData(data)
	if err != nil {
		return err
	}
	value, present := settings[enabledstate.SettingKey("zapret")]
	if !present {
		return nil
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil || enabled {
		return err
	}
	if _, err := outbounds.CheckZapretStartup(tx, false); err != nil {
		return fmt.Errorf("switch the Zapret binding OFF before disabling the component: %w", err)
	}
	return nil
}
