package model

// TLSCertificateProvider belongs to the TLS semantic owner. Options are an
// authenticated envelope, never cleartext JSON or an alternate base store.
type TLSCertificateProvider struct {
	Id              uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Tag             string `json:"tag" gorm:"not null;uniqueIndex"`
	Type            string `json:"type" gorm:"not null"`
	RuntimeMode     string `json:"runtimeMode" gorm:"not null"`
	OptionsEnvelope string `json:"-" gorm:"not null"`
}

func (TLSCertificateProvider) TableName() string { return "tls_certificate_providers" }
