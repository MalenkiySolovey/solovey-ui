package artifacts

import (
	"context"
	"errors"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

type ArtifactMetadataWriter interface {
	SaveArtifact(context.Context, *protectionrepository.ArtifactModel) error
}

// Service commits the filesystem set first and its DB metadata second. If the
// metadata write fails, the unpublished set is removed so it cannot be used by
// recovery without a matching journal record.
type Service struct {
	Storage *Storage
	Store   ArtifactMetadataWriter
}

func (s Service) WriteRevision(ctx context.Context, operationID, revision string, files map[string][]byte) (protectionrepository.ArtifactModel, error) {
	if s.Storage == nil || s.Store == nil {
		return protectionrepository.ArtifactModel{}, errors.New("artifact service is not initialized")
	}
	written, err := s.Storage.WriteRevision(operationID, revision, files)
	if err != nil {
		return protectionrepository.ArtifactModel{}, err
	}
	item := protectionrepository.ArtifactModel{
		OperationID: operationID, Revision: revision, Scope: "revision", RelativePath: written.RelativePath,
		ManifestSHA256: written.ManifestSHA256, Bytes: written.Bytes,
		CreatedAt: written.Manifest.CreatedAt, UpdatedAt: written.Manifest.CreatedAt,
	}
	if err := s.Store.SaveArtifact(ctx, &item); err != nil {
		_ = s.Storage.Remove(written.RelativePath)
		_ = s.Storage.Remove(filepathSlash("operations", operationID))
		return protectionrepository.ArtifactModel{}, err
	}
	return item, nil
}
