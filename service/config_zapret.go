package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

func (r *Runtime) installedZapretAdmitter(ctx context.Context, admitted *string) outbounds.ZapretBatchAdmitter {
	return func(targets []zapretactivation.Request) ([]zapretactivation.Grant, error) {
		if *admitted != "" || !r.zapretComponentStarted() {
			return nil, outbounds.ErrZapretPacketPathUnqualified
		}
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil {
			return nil, err
		}
		// Retain the batch identity if the broker publishes but the reply is lost.
		*admitted = hex.EncodeToString(token[:])
		target := zapretactivation.BatchRequest{BatchID: *admitted, Bindings: targets}
		var grant zapretactivation.BatchGrant
		client := broker.NewClient(broker.RolePanel)
		if _, err := client.Invoke(ctx, broker.Call{Verb: broker.VerbZapretAcquire, OperationID: "zapret-core-start", Payload: target, Timeout: 10 * time.Second}, &grant); err != nil {
			return nil, errors.Join(outbounds.ErrZapretPacketPathUnqualified, err)
		}
		if grant.BatchID != *admitted {
			return nil, outbounds.ErrZapretPacketPathUnqualified
		}
		return grant.Bindings, nil
	}
}

// The Core lifecycle lock also serializes the receipt. Keep it until the
// broker confirms retirement so an OFF save cannot report cleanup prematurely.
func (r *Runtime) retireZapretBatch() error {
	if r.zapretBatch == "" {
		return nil
	}
	client := broker.NewClient(broker.RolePanel)
	_, err := client.Invoke(context.Background(), broker.Call{Verb: broker.VerbZapretRelease, OperationID: "zapret-core-stop", Payload: zapretactivation.Retirement{BatchID: r.zapretBatch}, Timeout: 5 * time.Second}, nil)
	if err != nil {
		return fmt.Errorf("could not confirm retirement of the Zapret generation: %w", err)
	}
	r.zapretBatch = ""
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
			if err := assembly.ValidateCoreRestartFromDB(tx, ready); err != nil {
				return err
			}
			if bound {
				return validateInstalledZapret(tx)
			}
			return nil
		}
	}
	return s.validateNativeInboundCandidate(tx)
}

func validateInstalledZapret(tx *gorm.DB) error {
	requests, err := outbounds.ZapretRequests(tx)
	if err != nil || len(requests) == 0 {
		return err
	}
	client := broker.NewClient(broker.RolePanel)
	_, err = client.Invoke(tx.Statement.Context, broker.Call{Verb: broker.VerbZapretValidate, OperationID: "zapret-preflight",
		Payload: zapretactivation.Validation{Bindings: requests}, Timeout: 5 * time.Second}, nil)
	if err != nil {
		return errors.Join(outbounds.ErrZapretPacketPathUnqualified, err)
	}
	return nil
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
