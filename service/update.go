package service

import serviceupdate "github.com/MalenkiySolovey/solovey-ui/service/update"

type VersionService struct{}

type VersionInfo = serviceupdate.VersionInfo
type ReleaseTarget = serviceupdate.ReleaseTarget

func (s *VersionService) GetVersionInfo() VersionInfo {
	return serviceupdate.GetVersionInfo()
}

func (s *VersionService) CheckForChannel(channel string, force bool) VersionInfo {
	return serviceupdate.CheckForChannel(channel, force)
}

func (s *VersionService) ResolveTarget(channel string) (ReleaseTarget, error) {
	return serviceupdate.ResolveTarget(channel)
}

func versionIsNewer(candidate, current string) bool {
	return serviceupdate.VersionIsNewer(candidate, current)
}
