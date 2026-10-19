package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
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
	rotationRevision := ""
	if secretbox.IsEncrypted(row.Value) {
		sum := sha256.Sum256([]byte(row.Value))
		rotationRevision = hex.EncodeToString(sum[:])
	}
	view := access.View(row.Value != "", rotationRevision)
	if current, ok := componentSecretRegistration(access.Reference); !ok || current != token {
		return view, secretref.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return view, err
	}
	if access.Operation == secretref.Presence {
		return view, nil
	}
	// Decrypt this one captured row: replacement cannot mix old ciphertext with
	// a newer revision. Active owner use may finish with its admitted snapshot.
	local := &SettingService{database: db.WithContext(ctx)}
	value, err := local.decryptSettingBytes(access.Reference.ID, row.Value)
	defer common.WipeBytes(value)
	if err != nil {
		return view, secretref.ErrUnavailable
	}
	if current, ok := componentSecretRegistration(access.Reference); !ok || current != token {
		return view, secretref.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return view, err
	}
	// Admission audit is synchronous and code-only before any owner receives
	// plaintext. Underlying DB/codec/callback error text never enters the view.
	if err := (&AuditService{}).RecordSynchronousContext(ctx, AuditEvent{Actor: "component:" + access.Actor.Component,
		Event: "component_secret_use", Resource: "settings", Severity: AuditSeverityInfo,
		Details: map[string]any{"reference": view.Reference, "operation": access.Operation,
			"rotationRevision": view.RotationRevision, "correlation": view.Correlation}}); err != nil {
		return view, secretref.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return view, err
	}
	if err := use(value); err != nil {
		return view, secretref.ErrUseFailed
	}
	return view, nil
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
