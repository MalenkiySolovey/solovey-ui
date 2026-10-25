package service

import (
	"context"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownedvalue"
	"gorm.io/gorm"
)

const maxReferencedSecretBytes = 1 << 20

// AccessComponentSecret is a scoped adapter over registered encrypted settings.
// Owner writes, rotation, delete, backup and reseal keep their existing paths.
// The callback may use the value for its owner operation; it must not retain the
// borrowed byte slice. Existing clients needing a string own their scoped copy.
func (s *SettingService) AccessComponentSecret(ctx context.Context, access secretref.Access, use func([]byte) error) (secretref.View, error) {
	if ctx == nil || access.Validate() != nil || (access.Operation == secretref.UseForOwner) != (use != nil) {
		return secretref.View{}, secretref.ErrDenied
	}
	token, ok := componentSecretRegistration(access.Reference)
	if !ok {
		return secretref.View{}, secretref.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return secretref.View{}, err
	}
	db := s.settingDatabase()
	if db == nil {
		return secretref.View{}, secretref.ErrUnavailable
	}
	var row struct {
		Value string
		Bytes int64
	}
	err := db.WithContext(ctx).Model(&model.Setting{}).
		Select("substr(value,1,?) AS value, length(CAST(value AS BLOB)) AS bytes", maxReferencedSecretBytes+1).
		Where("key = ?", access.Reference.ID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row.Value, _ = defaultSettingValue(access.Reference.ID)
		row.Bytes = int64(len(row.Value))
	} else if err != nil {
		return secretref.View{}, secretref.ErrUnavailable
	}
	if row.Bytes > maxReferencedSecretBytes {
		return secretref.View{}, secretref.ErrUnavailable
	}
	view := ownedvalue.View(access, row.Value)
	if current, ok := componentSecretRegistration(access.Reference); !ok || current != token {
		return view, secretref.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return view, err
	}
	// Decrypt this one captured row: replacement cannot mix old ciphertext with
	// a newer revision. Active owner use may finish with its admitted snapshot.
	local := &SettingService{database: db.WithContext(ctx)}
	return ownedvalue.Access(ctx, access, row.Value, func() ([]byte, error) {
		return local.decryptSettingBytes(access.Reference.ID, row.Value)
	}, func(view secretref.View) error {
		if current, ok := componentSecretRegistration(access.Reference); !ok || current != token {
			return secretref.ErrDenied
		}
		// Admission audit is synchronous and code-only before any owner receives
		// plaintext. Underlying DB/codec/callback text never enters the view.
		return (&AuditService{}).RecordSynchronousContext(ctx, AuditEvent{Actor: "component:" + access.Actor.Component,
			Event: "component_secret_use", Resource: "settings", Severity: AuditSeverityInfo,
			Details: map[string]any{"reference": view.Reference, "operation": access.Operation,
				"rotationRevision": view.RotationRevision, "correlation": view.Correlation}})
	}, use)
}

func componentSecretRegistration(reference secretref.Reference) (uint64, bool) {
	settingContributions.RLock()
	defer settingContributions.RUnlock()
	registered, ok := settingContributions.entries[reference.Owner]
	if !ok {
		return 0, false
	}
	_, owned := registered.contribution.Encrypted[reference.ID]
	return registered.token, owned
}
