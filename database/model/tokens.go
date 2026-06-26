package model

type Tokens struct {
	Id          uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Desc        string `json:"desc" form:"desc"`
	Token       string `json:"-" form:"token"`
	TokenHash   string `json:"-" gorm:"index"`
	TokenPrefix string `json:"tokenPrefix"`
	Scope       string `json:"scope" gorm:"default:admin;not null"`
	Enabled     bool   `json:"enabled" gorm:"default:true;not null"`
	Expiry      int64  `json:"expiry" form:"expiry"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	LastUsedAt  int64  `json:"lastUsedAt"`
	LastUsedIP  string `json:"lastUsedIp"`
	UserId      uint   `json:"userId" form:"userId"`
	User        *User  `json:"user" gorm:"foreignKey:UserId;references:Id"`
}
