package sshmanagement

import "gorm.io/gorm"

func NewRepository(db *gorm.DB) Repository { return Repository{DB: func() *gorm.DB { return db }} }
