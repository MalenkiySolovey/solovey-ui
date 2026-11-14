package operations

import (
	"context"
	"errors"
)

func (m *Manager) admitMaintenance(ctx context.Context) (context.Context, func(), error) {
	if m.store == nil {
		return nil, nil, errors.New("protection journal owner is unavailable")
	}
	admitted, release, err := m.store.AdmitMaintenance(ctx)
	if err != nil {
		if release != nil {
			release()
		}
		return nil, nil, err
	}
	if admitted == nil || release == nil {
		if release != nil {
			release()
		}
		return nil, nil, errors.New("protection journal maintenance admission is unavailable")
	}
	if err := admitted.Err(); err != nil {
		release()
		return nil, nil, err
	}
	return admitted, release, nil
}
