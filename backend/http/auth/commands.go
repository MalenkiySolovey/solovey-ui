package auth

import (
	"context"
	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
)

// UserCommands is the account/token method set actually consumed by these
// routes. Implementations retain their transaction and post-commit authority.
type UserCommands interface {
	Authenticate(context.Context, string, string, string) (panelsecurity.AuthenticationResult, error)
	GetUsers() ([]panelsecurity.Administrator, error)
	AddUser(string, string, string, string) (panelsecurity.Administrator, error)
	DeleteUser(string, string, string) (panelsecurity.AdministratorDeletion, error)
	ChangePass(string, string, string, string) error
	LoadTokens() ([]byte, error)
	GetUserTokens(string) (*[]panelsecurity.APIToken, error)
	AddToken(string, int64, string, string) (string, error)
	DeleteToken(string, string) error
	SetTokenEnabled(string, string, bool) error
}

// SessionSettings owns the four existing lifetime/invalidation operations.
type SessionSettings interface {
	ResolveSessionLifetime() (panelsecurity.SessionLifetime, error)
	GetSessionGeneration() (string, error)
	RotateSessionGeneration() (string, error)
	GetSessionMaxAge() (int, error)
}
