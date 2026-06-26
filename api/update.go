package api

import updatehttp "github.com/MalenkiySolovey/solovey-ui/api/update"

func (a *APIHandler) updateDeps() updatehttp.Deps {
	return updatehttp.Deps{
		Settings:  &a.SettingService,
		Versions:  &a.VersionService,
		Manager:   a.PanelUpdateService,
		LoginUser: GetLoginUser,
		RemoteIP:  getRemoteIp,
		CheckPassword: func(user, password, remoteIP string) bool {
			found, _ := a.UserService.CheckUser(user, password, remoteIP)
			return found != nil
		},
		CheckRateLimit: checkLoginRateLimit,
		RecordFailure:  recordLoginFailure,
		ResetFailures:  resetLoginFailures,
		UserKey:        loginRateLimitUserKey,
		AllowCheck:     allowForcedUpdateCheck,
		Audit:          a.recordAudit,
		JSONObj:        jsonObj,
		JSONMsg:        jsonMsg,
	}
}
