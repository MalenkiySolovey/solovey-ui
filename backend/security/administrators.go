package security

// Administrator is the credential-free Panel account projection consumed by HTTP.
type Administrator struct {
	ID        uint
	SortOrder int
	Username  string
	LastLogin string
}

type AdministratorDeletion struct {
	User              Administrator
	DeletedTokenCount int64
}

// APIToken preserves the existing token-list response without carrying a token
// hash or a reusable bearer secret from the persistence owner into HTTP.
type APIToken struct {
	ID          uint             `json:"id"`
	Desc        string           `json:"desc"`
	TokenPrefix string           `json:"tokenPrefix"`
	Scope       string           `json:"scope"`
	Enabled     bool             `json:"enabled"`
	Expiry      int64            `json:"expiry"`
	CreatedAt   int64            `json:"createdAt"`
	UpdatedAt   int64            `json:"updatedAt"`
	LastUsedAt  int64            `json:"lastUsedAt"`
	LastUsedIP  string           `json:"lastUsedIp"`
	UserID      uint             `json:"userId"`
	User        *APITokenAccount `json:"user"`
}

// APITokenAccount retains the historical optional token owner JSON fields.
// Listing tokens currently leaves this relationship unpopulated.
type APITokenAccount struct {
	ID                    uint   `json:"id"`
	SortOrder             int    `json:"sortOrder"`
	Username              string `json:"username"`
	LastLogin             string `json:"lastLogin"`
	ForcePasswordReset    bool   `json:"forcePasswordReset"`
	PasswordPolicyVersion int    `json:"passwordPolicyVersion"`
	PasswordHashVersion   int    `json:"passwordHashVersion"`
	PasswordChangedAt     int64  `json:"passwordChangedAt"`
}

// APITokenAuthorization is the current principal and permission of a stored
// token. A cached token is only a candidate; the durable owner decides whether
// it still authorizes this request.
type APITokenAuthorization struct {
	ID          uint
	TokenPrefix string
	Scope       string
	Username    string
}
