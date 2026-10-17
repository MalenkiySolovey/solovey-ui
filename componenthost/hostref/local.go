// Package hostref defines the Local Host binding shared by read contracts.
// Remote Host registration, transport and identity resolution are not shipped.
package hostref

type Ref struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func Local() Ref { return Ref{Kind: "LOCAL", ID: "local"} }

func (r Ref) IsLocal() bool { return r == Local() }
