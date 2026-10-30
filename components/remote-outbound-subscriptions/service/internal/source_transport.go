//go:build !minimal

package remotesubservice

import (
	"context"
	"errors"
	"time"

	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"gorm.io/gorm"
)

var errDownloadOutbound = errors.New("SUBSCRIPTION_DOWNLOAD_OUTBOUND_UNAVAILABLE")

type DownloadOutbound struct {
	ID         uint                    `json:"id"`
	Tag        string                  `json:"tag"`
	Type       string                  `json:"type"`
	Capability entitycapabilities.Fact `json:"capability"`
}

func (s *Service) DownloadOutbounds(db *gorm.DB) ([]DownloadOutbound, error) {
	var rows []model.Outbound
	if err := db.Select("id", "tag", "type", "options").Order("sort_order, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]DownloadOutbound, 0, len(rows))
	for _, row := range rows {
		result = append(result, DownloadOutbound{ID: row.Id, Tag: row.Tag, Type: row.Type,
			Capability: entitycapabilities.ResolveResource("outbounds", row.Type, row.Options)})
	}
	return result, nil
}

func downloadOutbound(db *gorm.DB, id uint) (model.Outbound, error) {
	var row model.Outbound
	if db.First(&row, id).Error != nil || !entitycapabilities.ResolveResource("outbounds", row.Type, row.Options).Available {
		return row, errDownloadOutbound
	}
	return row, nil
}

func validateDownloadOutbound(db *gorm.DB, id, previous uint) error {
	// Historical references remain owned across provider transitions. Only a
	// newly selected reference requires current availability; fetch always does.
	if id == 0 || id == previous {
		return nil
	}
	_, err := downloadOutbound(db, id)
	return err
}

func (s *Service) fetchSource(ctx context.Context, db *gorm.DB, source remotesub.RemoteOutboundSubscription) (*remotesub.FetchedSubscription, error) {
	options := s.fetchOptions()
	if source.DownloadOutboundID != 0 {
		if source.SourceKind() != remotesub.SourceRemote || s.OutboundHTTP == nil {
			return nil, errDownloadOutbound
		}
		row, err := downloadOutbound(db, source.DownloadOutboundID)
		if err != nil {
			return nil, err
		}
		client, err := s.OutboundHTTP.CoreHTTPClient(row.Tag, 15*time.Second)
		if err != nil || client == nil {
			return nil, errDownloadOutbound
		}
		defer client.CloseIdleConnections()
		options.OutboundClient = client
	}
	return remotesub.FetchSourceContext(ctx, db, source, options)
}
