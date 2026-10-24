// Package facts contains the engine-free value projected by the runtime
// registry. Declarations and capability decisions remain in their owners.
package facts

type Fact struct {
	Category           string `json:"category"`
	Type               string `json:"type"`
	Known              bool   `json:"known"`
	Registered         bool   `json:"registered"`
	Compiled           bool   `json:"compiled"`
	SupportedByProduct bool   `json:"supportedByProduct"`
	BuildTag           string `json:"buildTag,omitempty"`
	Platform           string `json:"platform,omitempty"`
	Reason             string `json:"reason,omitempty"`
	AuthenticatedUsers bool   `json:"authenticatedUsers,omitempty"`
	RuntimeDependency  string `json:"runtimeDependency,omitempty"`
}

func (f Fact) Available() bool { return f.Known && f.Registered && f.Compiled && f.SupportedByProduct }
