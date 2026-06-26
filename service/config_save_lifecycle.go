package service

type configCoreLifecycle interface {
	startCoreLocked(force bool) error
	restartCoreLocked() error
}

type configServiceCoreLifecycle struct {
	service *ConfigService
}

func (s *ConfigService) configCoreLifecycle() configCoreLifecycle {
	if s.coreLifecycle != nil {
		return s.coreLifecycle
	}
	return configServiceCoreLifecycle{service: s}
}

func (l configServiceCoreLifecycle) startCoreLocked(force bool) error {
	return l.service.startCoreLocked(force)
}

func (l configServiceCoreLifecycle) restartCoreLocked() error {
	return l.service.restartCoreLocked()
}
