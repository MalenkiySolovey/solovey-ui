package service

func saveClientsConfigObject(s *ConfigService, req configSaveRequest, plan *configSavePlan) error {
	inboundIds, err := s.ClientService.Save(req.tx, req.action, req.data, req.hostname)
	if err != nil {
		return err
	}
	if len(inboundIds) > 0 {
		plan.ApplyCascade(configSaveCascadeClientInboundMembershipChanged)
	}
	return nil
}

func saveTLSConfigObject(s *ConfigService, req configSaveRequest, plan *configSavePlan) error {
	if err := s.TlsService.Save(req.tx, req.action, req.data, req.hostname); err != nil {
		return err
	}
	plan.ApplyCascade(configSaveCascadeTLSChanged)
	return nil
}

func saveInboundsConfigObject(s *ConfigService, req configSaveRequest, plan *configSavePlan) error {
	if err := s.InboundService.Save(req.tx, req.action, req.data, req.initUsers, req.hostname); err != nil {
		return err
	}
	plan.ApplyCascade(configSaveCascadeInboundChanged)
	return nil
}
