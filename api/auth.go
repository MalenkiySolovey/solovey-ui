package api

import (
	authhttp "github.com/MalenkiySolovey/solovey-ui/api/auth"
	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func (a *ApiService) authHandler() *authhttp.Handler {
	return authhttp.NewHandler(a.authDeps())
}

func (a *ApiService) authDeps() authhttp.Deps {
	return authhttp.Deps{
		UserService:    a.UserService,
		SettingService: a.SettingService,
		NotifyEvent:    service.NotifyPanelEvent,
		NotifyAuthenticationEvent: func(event, user, sessionRevision string, identity clientidentity.V1) {
			service.NotifyPanelAuthenticationEvent(service.PanelAuthenticationEventV1{Event: event, User: user, SessionRevision: sessionRevision, ClientIdentity: identity})
		},
		ClientIdentity:           RequestClientIdentity,
		JSONObj:                  jsonObj,
		JSONMsg:                  jsonMsg,
		JSONMsgObj:               jsonMsgObj,
		Audit:                    a.recordAudit,
		LoginUser:                GetLoginUser,
		SetLoginUser:             SetLoginUser,
		SetLoginSecurity:         SetLoginSecurity,
		ClearSession:             ClearSession,
		RemoteIP:                 getRemoteIp,
		CheckLoginRateLimit:      checkLoginRateLimit,
		RecordLoginFailure:       recordLoginFailure,
		ResetLoginFailures:       resetLoginFailures,
		LoginRateLimitUserKey:    loginRateLimitUserKey,
		LoginUsernameTarpitDelay: loginUsernameTarpitDelay,
	}
}
