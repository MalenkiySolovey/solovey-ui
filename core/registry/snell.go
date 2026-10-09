package registry

// SnellVersion is the product's pinned 1.14.2 protocol contract. These are
// value facts (including arrays), so callers cannot mutate the authority.
type SnellVersion struct {
	Version       int  `json:"version"`
	ClientVersion int  `json:"clientVersion"`
	PSKMinBytes   int  `json:"pskMinBytes"`
	PSKMaxBytes   int  `json:"pskMaxBytes"` // zero means no core upper bound
	Obfuscation   bool `json:"obfuscation"`
}

type SnellCapability struct {
	Versions        [2]SnellVersion `json:"versions"`
	ObfsModes       [3]string       `json:"obfsModes"`
	Modes           [3]string       `json:"modes"`
	UserKeyMaxBytes int             `json:"userKeyMaxBytes"`
	URI             bool            `json:"uri"`
}

func SnellContract(category string) SnellCapability {
	legacy := 4
	if category == "inbounds" {
		legacy = 5
	}
	return SnellCapability{
		Versions:  [2]SnellVersion{{Version: legacy, ClientVersion: 4, PSKMinBytes: 1, Obfuscation: true}, {Version: 6, ClientVersion: 6, PSKMinBytes: 12, PSKMaxBytes: 255}},
		ObfsModes: [3]string{"none", "http", "tls"}, Modes: [3]string{"default", "unshaped", "unsafe-raw"}, UserKeyMaxBytes: 255,
	}
}
