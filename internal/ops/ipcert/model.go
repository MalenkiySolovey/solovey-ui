package ipcert

type Status struct {
	Enabled       bool    `json:"enabled"`
	TargetIP      string  `json:"targetIp"`
	ApplyTarget   string  `json:"applyTarget"`
	Issued        bool    `json:"issued"`
	NotAfter      string  `json:"notAfter"`
	LastIssue     string  `json:"lastIssue"`
	DaysRemaining float64 `json:"daysRemaining"`
	CertPath      string  `json:"certPath"`
}
