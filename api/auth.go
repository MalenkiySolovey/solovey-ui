package api

import (
	authhttp "github.com/MalenkiySolovey/solovey-ui/api/auth"
)

func (a *ApiService) authHandler() *authhttp.Handler {
	return authhttp.NewHandler(a.authDeps())
}

func (a *ApiService) authDeps() authhttp.Deps {
	return authhttp.Deps{
		UserService:              a.UserService,
		SettingService:           a.SettingService,
		TelegramService:          a.TelegramService,
		JSONObj:                  jsonObj,
		JSONMsg:                  jsonMsg,
		JSONMsgObj:               jsonMsgObj,
		Audit:                    a.recordAudit,
		LoginUser:                GetLoginUser,
		SetLoginUser:             SetLoginUser,
		ClearSession:             ClearSession,
		RemoteIP:                 getRemoteIp,
		CheckLoginRateLimit:      checkLoginRateLimit,
		RecordLoginFailure:       recordLoginFailure,
		ResetLoginFailures:       resetLoginFailures,
		LoginRateLimitUserKey:    loginRateLimitUserKey,
		LoginUsernameTarpitDelay: loginUsernameTarpitDelay,
	}
}
