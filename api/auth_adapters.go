package api

import (
	"context"

	authhttp "github.com/MalenkiySolovey/solovey-ui/backend/http/auth"
	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

// administratorCommands projects committed account/token results for HTTP. Each
// mutation is delegated once to its existing authoritative service operation.
type administratorCommands struct{ service *service.UserService }

var _ authhttp.UserCommands = administratorCommands{}
var _ authhttp.SessionSettings = (*service.SettingService)(nil)

func (a administratorCommands) Authenticate(ctx context.Context, username, password, remoteIP string) (panelsecurity.AuthenticationResult, error) {
	return a.service.Authenticate(ctx, username, password, remoteIP)
}

func (a administratorCommands) GetUsers() ([]panelsecurity.Administrator, error) {
	users, err := a.service.GetUsers()
	if err != nil {
		return nil, err
	}
	result := make([]panelsecurity.Administrator, 0, len(*users))
	for _, user := range *users {
		result = append(result, administratorProjection(user))
	}
	return result, nil
}

func (a administratorCommands) AddUser(actor, currentPass, username, password string) (panelsecurity.Administrator, error) {
	user, err := a.service.AddUser(actor, currentPass, username, password)
	if err != nil {
		return panelsecurity.Administrator{}, err
	}
	return administratorProjection(*user), nil
}

func (a administratorCommands) DeleteUser(actor, currentPass, targetID string) (panelsecurity.AdministratorDeletion, error) {
	result, err := a.service.DeleteUser(actor, currentPass, targetID)
	if err != nil {
		return panelsecurity.AdministratorDeletion{}, err
	}
	return panelsecurity.AdministratorDeletion{User: administratorProjection(result.User), DeletedTokenCount: result.DeletedTokenCount}, nil
}

func (a administratorCommands) ChangePass(username, oldPass, newUsername, newPass string) error {
	return a.service.ChangePass(username, oldPass, newUsername, newPass)
}

func (a administratorCommands) LoadTokens() ([]byte, error) { return a.service.LoadTokens() }

func (a administratorCommands) GetUserTokens(username string) (*[]panelsecurity.APIToken, error) {
	tokens, err := a.service.GetUserTokens(username)
	if err != nil || tokens == nil {
		return nil, err
	}
	var result []panelsecurity.APIToken
	if *tokens != nil {
		result = make([]panelsecurity.APIToken, 0, len(*tokens))
	}
	for _, token := range *tokens {
		result = append(result, tokenProjection(token))
	}
	return &result, nil
}

func (a administratorCommands) AddToken(username string, expiry int64, desc, scope string) (string, error) {
	return a.service.AddToken(username, expiry, desc, scope)
}

func (a administratorCommands) DeleteToken(username, id string) error {
	return a.service.DeleteToken(username, id)
}

func (a administratorCommands) SetTokenEnabled(username, id string, enabled bool) error {
	return a.service.SetTokenEnabled(username, id, enabled)
}

func administratorProjection(user model.User) panelsecurity.Administrator {
	return panelsecurity.Administrator{ID: user.Id, SortOrder: user.SortOrder, Username: user.Username, LastLogin: user.LastLogins}
}

func tokenProjection(token model.Tokens) panelsecurity.APIToken {
	result := panelsecurity.APIToken{ID: token.Id, Desc: token.Desc, TokenPrefix: token.TokenPrefix, Scope: token.Scope,
		Enabled: token.Enabled, Expiry: token.Expiry, CreatedAt: token.CreatedAt, UpdatedAt: token.UpdatedAt,
		LastUsedAt: token.LastUsedAt, LastUsedIP: token.LastUsedIP, UserID: token.UserId}
	if user := token.User; user != nil {
		result.User = &panelsecurity.APITokenAccount{ID: user.Id, SortOrder: user.SortOrder, Username: user.Username, LastLogin: user.LastLogins,
			ForcePasswordReset: user.ForcePasswordReset, PasswordPolicyVersion: user.PasswordPolicyVersion,
			PasswordHashVersion: user.PasswordHashVersion, PasswordChangedAt: user.PasswordChangedAt}
	}
	return result
}
