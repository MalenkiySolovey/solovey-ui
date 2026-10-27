package service

import (
	"context"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

// UseCallRoom shares config/restore serialization. The callback must only copy
// the bounded credential for the authorized response; it cannot mutate config.
func (s *ConfigService) UseCallRoom(ctx context.Context, inboundID uint, generation string, use func([]byte) error) error {
	if s == nil || ctx == nil || s.coreInstance() == nil || s.runtime().restart() == nil {
		return coreinboundcontrol.ErrCallRoomUnavailable
	}
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return coreinboundcontrol.ErrCallRoomUnavailable
	}
	defer release()
	return s.runtime().restart().RunBlockingContext(ctx, func() error {
		return s.CoreInboundControl().UseCallRoom(ctx, inboundID, generation, use)
	})
}
