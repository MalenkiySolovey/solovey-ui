// Package ownerdb adapts existing owner rows to SecretRef access. It reuses the
// settings key lifecycle and common audit sink; it owns no secret table or API.
package ownerdb

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/internal/auditrecord"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownedvalue"
	settingsstore "github.com/MalenkiySolovey/solovey-ui/internal/settings/store"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

// Reference is derived by trusted owner code, after resolving row identity.
// field is a fixed schema declaration, never a request-supplied lookup path.
func Reference(owner string, rowID uint, field string) (secretref.Reference, error) {
	ref := secretref.NewReference(owner, fmt.Sprintf("record:%d:%s", rowID, field))
	if rowID == 0 || presence(ref).Validate() != nil {
		return secretref.Reference{}, secretref.ErrDenied
	}
	return ref, nil
}

func presence(ref secretref.Reference) secretref.Access {
	return secretref.Access{Reference: ref, Actor: secretref.Actor{Component: ref.Owner}, Operation: secretref.Presence, IntentID: "credential-presence"}
}

func View(ref secretref.Reference, captured string) secretref.View {
	return ownedvalue.View(presence(ref), captured)
}

// Apply seals portable new owner values with this database's existing master.
// This is the same candidate-bound backend used by certificate providers;
// approved environment/legacy candidates remain available for reads. An
// unchanged mutation neither creates a key nor changes ciphertext.
func Apply(db *gorm.DB, ref secretref.Reference, captured string, mutation json.RawMessage) (string, error) {
	if db == nil {
		return "", secretref.ErrUnavailable
	}
	var result string
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = ownedvalue.Apply(ref, captured, mutation, func(value, aad string) (string, error) {
			master, err := settingsstore.EnsureMasterSecret(tx)
			defer common.WipeBytes(master)
			if err != nil {
				return "", err
			}
			candidates, err := settingsstore.Codec(tx).SettingsSecretboxCandidates()
			if err != nil || len(candidates) == 0 {
				return "", secretref.ErrUnavailable
			}
			return candidates[0].Box.EncryptString(value, aad)
		})
		if err != nil || result == captured {
			return err
		}
		event := "owner_secret_replace"
		if result == "" {
			event = "owner_secret_clear"
		}
		view := View(ref, result)
		if err := auditrecord.RecordTransaction(tx, auditrecord.Event{Actor: "owner:" + ref.Owner,
			Event: event, Resource: ref.Owner, Severity: auditrecord.SeverityInfo,
			Details: map[string]any{"reference": view.Reference, "present": view.Present, "rotationRevision": view.RotationRevision},
		}, time.Now().Unix()); err != nil {
			return secretref.ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

// Access resolves a captured field only for its owning operation. The caller
// supplies the same admitted database transaction used for its state. Read-only
// inspection uses View; inability to record use fails closed before plaintext
// reaches a consumer. There is no process-global or foreign-database fallback.
func Access(ctx context.Context, db *gorm.DB, request secretref.Access, captured string, use func([]byte) error) (secretref.View, error) {
	if db == nil {
		return secretref.View{}, secretref.ErrUnavailable
	}
	if captured != "" && !secretbox.IsEncrypted(captured) {
		return secretref.View{}, secretref.ErrUnavailable
	}
	return ownedvalue.Access(ctx, request, captured, func() ([]byte, error) {
		return settingsstore.Codec(db).DecryptBytes(ownedvalue.AAD(request.Reference), captured)
	}, func(view secretref.View) error {
		return auditrecord.RecordTransaction(db.WithContext(ctx), auditrecord.Event{
			Actor: "owner:" + request.Actor.Component, Event: "owner_secret_use", Resource: request.Reference.Owner,
			Severity: auditrecord.SeverityInfo, Details: map[string]any{"reference": view.Reference,
				"operation": request.Operation, "rotationRevision": view.RotationRevision, "correlation": view.Correlation},
		}, time.Now().Unix())
	}, use)
}
