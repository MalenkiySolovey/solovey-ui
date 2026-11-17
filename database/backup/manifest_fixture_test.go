package backup

type backupManifestRecord struct {
	Scope   string `gorm:"primaryKey;size:16"`
	Payload []byte `gorm:"not null"`
	Digest  string `gorm:"size:64;not null"`
}

func (backupManifestRecord) TableName() string { return BackupManifestTable }
