package coreinboundcontrol

import (
	"context"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

var ErrCallRoomUnavailable = errors.New("call room unavailable")

type privateCallRoomReader interface {
	UseCallRoom(context.Context, string, string, string, func([]byte) error) error
}

// UseCallRoom binds the native room to the current canonical inbound and its
// hydrated runtime options. The room is never included in a public snapshot.
func (s *Service) UseCallRoom(ctx context.Context, inboundID uint, generation string, use func([]byte) error) error {
	if s == nil || s.db == nil || ctx == nil || ctx.Err() != nil || inboundID == 0 || generation == "" || use == nil {
		return ErrCallRoomUnavailable
	}
	reader, ok := s.effective.(privateCallRoomReader)
	if !ok {
		return ErrCallRoomUnavailable
	}
	var inbound model.Inbound
	if s.db.WithContext(ctx).First(&inbound, inboundID).Error != nil || inbound.Type != "call" {
		return ErrCallRoomUnavailable
	}
	content, digest, err := s.expectedRuntimeOptions(ctx, &inbound)
	clear(content)
	if err != nil || len(digest) != 64 {
		return ErrCallRoomUnavailable
	}
	if err := reader.UseCallRoom(ctx, generation, inbound.Tag, digest, use); err != nil {
		return ErrCallRoomUnavailable
	}
	return nil
}
