package registry

import "context"

// QUICParent is a transport-owner projection. Principal is internal verified
// protocol output, never an address or a public API identity.
type QUICParent struct {
	ID        string
	Principal string
	CreatedAt int64
}

// QUICParentControl is optional and belongs to one constructed inbound. The
// runtime holds only a generation lease and delegates to that lifecycle owner.
type QUICParentControl interface {
	QUICParents(limit int) ([]QUICParent, int)
	QUICParent(id string) (QUICParent, bool)
	CloseQUICParent(context.Context, string, string) error
}
