package model

// FailoverMemberState is observability state only. The running selector remains
// authoritative for the currently active member.
type FailoverMemberState struct {
	GroupTag    string `gorm:"column:group_tag;primaryKey"`
	MemberTag   string `gorm:"column:member_tag;primaryKey"`
	Healthy     bool   `gorm:"column:healthy;not null;default:false"`
	ConsecUp    int    `gorm:"column:consec_up;not null;default:0"`
	ConsecDown  int    `gorm:"column:consec_down;not null;default:0"`
	LastProbeAt int64  `gorm:"column:last_probe_at;not null;default:0"`
}

func (FailoverMemberState) TableName() string { return "failover_state" }
