package model

type ComponentMigration struct {
	Id          uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	ComponentID string `json:"componentId" gorm:"size:96;not null;uniqueIndex:idx_component_migrations_component_version"`
	Name        string `json:"name"`
	Version     string `json:"version" gorm:"size:64;not null;uniqueIndex:idx_component_migrations_component_version"`
	Delivery    string `json:"delivery"`
	AppliedAt   int64  `json:"appliedAt" gorm:"not null"`
}
