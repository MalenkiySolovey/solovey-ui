package service

import (
	"encoding/json"

	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"gorm.io/gorm"
)

func (s *ConfigService) applyConfigSaveMutation(tx *gorm.DB, plan *configSavePlan, obj string, act string, data json.RawMessage, initUsers string, hostname string) error {
	req := singboxapply.MutationRequest{
		Tx:        tx,
		Object:    obj,
		Action:    act,
		Data:      data,
		InitUsers: initUsers,
		Hostname:  hostname,
	}
	return applyConfigSaveObject(s, req, &plan.Plan)
}

func applyConfigSaveObject(s *ConfigService, req singboxapply.MutationRequest, plan *singboxapply.Plan) error {
	return singboxapply.ApplyMutation(configSaveExecutor{service: s}, req, plan)
}
