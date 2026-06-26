package model

type ClientIP struct {
	Id         uint64  `json:"id" gorm:"primaryKey;autoIncrement"`
	ClientName string  `json:"clientName" gorm:"index:idx_client_ips_client_hash,unique"`
	IP         string  `json:"ip"`
	IPHash     string  `json:"ipHash,omitempty" gorm:"index:idx_client_ips_client_hash,unique"`
	IPDisplay  *string `json:"ipDisplay,omitempty"`
	FirstSeen  int64   `json:"firstSeen"`
	LastSeen   int64   `json:"lastSeen" gorm:"index"`
}
