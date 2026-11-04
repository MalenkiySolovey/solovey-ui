package devices

import (
	"math"
	"reflect"

	entitytypes "github.com/MalenkiySolovey/solovey-ui/internal/entities/types"
	"gorm.io/gorm"
)

type TrafficDelta struct {
	UserID   entitytypes.VPNUserID
	Up, Down int64
}

func addCounter(a, b int64) int64 {
	if a < 0 || b < 0 {
		return a
	}
	if b > math.MaxInt64-a {
		return math.MaxInt64
	}
	return a + b
}
func counterDelta(current, previous int64) int64 {
	if current < 0 || previous < 0 {
		return 0
	}
	if current < previous {
		return current
	}
	return current - previous
}

// Account only associates immutable public keys with the current User/device
// identity. Old keys, another endpoint and an obsolete runtime cannot become
// authority for a new device. The caller fences the captured Core generation.
func Account(tx *gorm.DB, endpointID uint, generation string, observedAt int64, samples map[string]Observation) ([]TrafficDelta, error) {
	if generation == "" || len(generation) > 128 || observedAt <= 0 || len(samples) > 4096 {
		return nil, ErrInvalid
	}
	owners, err := clients(tx)
	if err != nil {
		return nil, err
	}
	deltas := []TrafficDelta{}
	for _, client := range owners {
		root, state, err := load(client.Config)
		if err != nil {
			continue
		}
		changed := false
		var up, down int64
		for i := range state.Devices {
			record := &state.Devices[i]
			if record.EndpointID != endpointID {
				continue
			}
			before := *record
			sample, present := samples[record.PublicKey]
			if record.Observation.Generation != generation || record.Observation.PublicKey != record.PublicKey {
				record.Observation = Observation{}
			}
			if present {
				if sample.Received < 0 || sample.Sent < 0 || sample.LastHandshake < 0 {
					return nil, ErrInvalid
				}
				previous := record.Observation
				if previous.Generation != generation || previous.PublicKey != record.PublicKey {
					previous = Observation{}
				}
				u, d := counterDelta(sample.Received, previous.Received), counterDelta(sample.Sent, previous.Sent)
				up, down = addCounter(up, u), addCounter(down, d)
				record.Up, record.Down = addCounter(record.Up, u), addCounter(record.Down, d)
				record.Observation = sample
			}
			record.Observation.Generation = generation
			record.Observation.PublicKey = record.PublicKey
			record.Observation.ObservedAt = observedAt
			record.Observation.Present = present
			if !present && record.DeletedAt > 0 && record.ReleasedAt == 0 {
				record.ReleasedAt = observedAt
			}
			if !reflect.DeepEqual(before, *record) {
				changed = true
			}
		}
		if !changed {
			continue
		}
		if err := save(tx, client, root, state); err != nil {
			return nil, err
		}
		// Preserve the existing aggregate counter range before the common
		// accounting owner applies deltas in this same write transaction.
		if client.Up < 0 || client.Down < 0 {
			return nil, ErrInvalid
		}
		up = min(up, math.MaxInt64-client.Up)
		down = min(down, math.MaxInt64-client.Down)
		if up > 0 || down > 0 {
			deltas = append(deltas, TrafficDelta{UserID: client.VPNUserID(), Up: up, Down: down})
		}
	}
	return deltas, nil
}
