package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/events"
)

var ErrScoreNotFound = errors.New("server-protection score state not found")

func (r *Repository) AppendBatch(ctx context.Context, values []events.ProbeEvent) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	if len(values) == 0 {
		return nil
	}
	settings, _, err := r.LoadSettings(ctx)
	if err != nil {
		return err
	}
	models := make([]ProbeEventModel, 0, len(values))
	for _, value := range values {
		if value.ResourceID == "" || value.DedupeKey == "" {
			return errors.New("probe event resource id and dedupe key are required")
		}
		if err := value.ResourceKind.Validate(); err != nil {
			return err
		}
		if err := value.SignalKind.Validate(); err != nil {
			return err
		}
		if err := value.Action.Validate(); err != nil {
			return err
		}
		meta := value.SafeMeta.Bounded(settings.SafeMetaMaxBytes)
		if err := meta.Validate(); err != nil {
			return err
		}
		encoded, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		if len(encoded) > settings.SafeMetaMaxBytes {
			return fmt.Errorf("safe metadata exceeds %d bytes after bounding", settings.SafeMetaMaxBytes)
		}
		if value.SourcePrefix != "" {
			prefix, err := netip.ParsePrefix(value.SourcePrefix)
			if err != nil || prefix.String() != value.SourcePrefix {
				return fmt.Errorf("source prefix %q is not canonical", value.SourcePrefix)
			}
		}
		observedAt := value.ObservedAt
		if observedAt.IsZero() {
			observedAt = time.Now()
		}
		models = append(models, ProbeEventModel{
			ResourceID: value.ResourceID, ResourceKind: string(value.ResourceKind),
			SourceIPCIDR: value.SourcePrefix, IPFamily: optionalIPFamily(value.IPFamily),
			SignalKind: string(value.SignalKind), ScoreDelta: value.ScoreDelta,
			Action: string(value.Action), SafeMetaJSON: encoded, SafeMetaBytes: len(encoded),
			ObservedAt: observedAt.Unix(), DedupeKey: value.DedupeKey,
		})
	}
	return r.db.WithContext(ctx).CreateInBatches(models, 100).Error
}
