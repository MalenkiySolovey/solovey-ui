package model

type User struct {
	Id                 uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	SortOrder          int    `json:"sortOrder" form:"sortOrder" gorm:"column:sort_order;default:0;not null;index"`
	Username           string `json:"username" form:"username"`
	Password           string `json:"password" form:"password"`
	LastLogins         string `json:"lastLogin"`
	ForcePasswordReset bool   `json:"forcePasswordReset" form:"forcePasswordReset" gorm:"column:force_password_reset;default:false;not null"`
}
