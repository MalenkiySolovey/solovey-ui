package model

type User struct {
	Id                    uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	SortOrder             int    `json:"sortOrder" form:"sortOrder" gorm:"column:sort_order;default:0;not null;index"`
	Username              string `json:"username" form:"username"`
	Password              string `json:"-" form:"password"`
	LastLogins            string `json:"lastLogin"`
	ForcePasswordReset    bool   `json:"forcePasswordReset" form:"forcePasswordReset" gorm:"column:force_password_reset;default:false;not null"`
	PasswordPolicyVersion int    `json:"passwordPolicyVersion" gorm:"column:password_policy_version;default:0;not null"`
	PasswordHashVersion   int    `json:"passwordHashVersion" gorm:"column:password_hash_version;default:0;not null"`
	CredentialGeneration  uint64 `json:"-" gorm:"column:credential_generation;default:1;not null"`
	MFAGeneration         uint64 `json:"-" gorm:"column:mfa_generation;default:1;not null"`
	PasswordChangedAt     int64  `json:"passwordChangedAt" gorm:"column:password_changed_at;default:0;not null"`
}
