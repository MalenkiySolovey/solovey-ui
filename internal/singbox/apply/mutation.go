package apply

import (
	"encoding/json"

	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type MutationExecutor interface {
	SaveClients(tx *gorm.DB, action string, data json.RawMessage, hostname string) ([]uint, error)
	SaveTLS(tx *gorm.DB, action string, data json.RawMessage, hostname string) error
	SaveInbounds(tx *gorm.DB, action string, data json.RawMessage, initUsers string, hostname string) (*Change, error)
	SaveOutbounds(tx *gorm.DB, action string, data json.RawMessage) (*Change, error)
	SaveServices(tx *gorm.DB, action string, data json.RawMessage) (*Change, error)
	SaveEndpoints(tx *gorm.DB, action string, data json.RawMessage) (*Change, error)
	ConfigBlobChanged(tx *gorm.DB, data json.RawMessage) (bool, error)
	SaveBaseConfig(tx *gorm.DB, data json.RawMessage) error
	SaveSettings(tx *gorm.DB, data json.RawMessage) error
}

func NewMutationRouter(executor MutationExecutor) Router {
	return NewRouter(map[Object]Handler{
		ObjectClients:   mutationHandler(executor, saveClientsConfigObject),
		ObjectTLS:       mutationHandler(executor, saveTLSConfigObject),
		ObjectInbounds:  mutationHandler(executor, saveInboundsConfigObject),
		ObjectOutbounds: mutationHandler(executor, saveOutboundsConfigObject),
		ObjectServices:  mutationHandler(executor, saveServicesConfigObject),
		ObjectEndpoints: mutationHandler(executor, saveEndpointsConfigObject),
		ObjectConfig:    mutationHandler(executor, saveBaseConfigObject),
		ObjectSettings:  mutationHandler(executor, saveSettingsConfigObject),
	})
}

func ApplyMutation(executor MutationExecutor, req MutationRequest, plan *Plan) error {
	if executor == nil {
		return common.NewError("missing config save executor")
	}
	return NewMutationRouter(executor).Apply(req, plan)
}

func MutationHandlerObjectStrings() []string {
	return SupportedObjectStrings()
}

type mutationHandlerFunc func(MutationExecutor, MutationRequest, *Plan) error

func mutationHandler(executor MutationExecutor, handler mutationHandlerFunc) Handler {
	return func(req MutationRequest, plan *Plan) error {
		if executor == nil {
			return common.NewError("missing config save executor")
		}
		return handler(executor, req, plan)
	}
}

func saveClientsConfigObject(executor MutationExecutor, req MutationRequest, plan *Plan) error {
	inboundIDs, err := executor.SaveClients(req.Tx, req.Action, req.Data, req.Hostname)
	if err != nil {
		return err
	}
	if len(inboundIDs) > 0 {
		plan.IncludeSaveObjects(ObjectInbounds)
		plan.MergeInboundChange(&Change{ReloadIDs: inboundIDs})
	}
	return nil
}

func saveTLSConfigObject(executor MutationExecutor, req MutationRequest, plan *Plan) error {
	if err := executor.SaveTLS(req.Tx, req.Action, req.Data, req.Hostname); err != nil {
		return err
	}
	plan.ApplyCascade(CascadeTLSChanged)
	return nil
}

func saveInboundsConfigObject(executor MutationExecutor, req MutationRequest, plan *Plan) error {
	change, err := executor.SaveInbounds(req.Tx, req.Action, req.Data, req.InitUsers, req.Hostname)
	if err != nil {
		return err
	}
	plan.IncludeSaveObjects(ObjectClients)
	plan.MergeInboundChange(change)
	return nil
}

func saveOutboundsConfigObject(executor MutationExecutor, req MutationRequest, plan *Plan) error {
	change, err := executor.SaveOutbounds(req.Tx, req.Action, req.Data)
	if err != nil {
		return err
	}
	plan.MergeOutboundChange(change)
	return nil
}

func saveServicesConfigObject(executor MutationExecutor, req MutationRequest, plan *Plan) error {
	change, err := executor.SaveServices(req.Tx, req.Action, req.Data)
	if err != nil {
		return err
	}
	plan.MergeServiceChange(change)
	return nil
}

func saveEndpointsConfigObject(executor MutationExecutor, req MutationRequest, plan *Plan) error {
	change, err := executor.SaveEndpoints(req.Tx, req.Action, req.Data)
	if err != nil {
		return err
	}
	plan.MergeEndpointChange(change)
	return nil
}

func saveBaseConfigObject(executor MutationExecutor, req MutationRequest, plan *Plan) error {
	if err := singboxconfig.ValidateLogOutput(req.Data); err != nil {
		return err
	}
	changed, err := executor.ConfigBlobChanged(req.Tx, req.Data)
	if err != nil {
		return err
	}
	if err := executor.SaveBaseConfig(req.Tx, req.Data); err != nil {
		return err
	}
	if changed {
		plan.ApplyCascade(CascadeCoreRuntimeChanged)
	}
	return nil
}

func saveSettingsConfigObject(executor MutationExecutor, req MutationRequest, plan *Plan) error {
	return executor.SaveSettings(req.Tx, req.Data)
}
