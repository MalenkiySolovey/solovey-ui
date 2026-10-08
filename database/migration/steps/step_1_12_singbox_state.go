package steps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityvalidation "github.com/MalenkiySolovey/solovey-ui/internal/entities"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/assembly"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/formats"
	"gorm.io/gorm"
)

const SingBoxStateStepID = "core-1.12-singbox-stored-state"

var SingBoxStateChecksum = singBoxStateChecksum()

// CoreOptions injects an owner-managed ephemeral file projection. It may only
// supply the already verified private archive's file facts for offline build;
// the canonical candidate and durable storage are not replaced by it.
type CoreOptions struct {
	ProjectRuntimeFiles func(*gorm.DB, []byte) ([]byte, error)
	ReportFindings      func([]diagnostics.Finding)
}

// The existing sequential migration plan orders owner-local staging. Every
// write is in the caller's unpublished transaction, before version publication.
func upgradeSingBoxStoredState(tx *gorm.DB, options CoreOptions) error {
	for _, stage := range []func(*gorm.DB) ([]diagnostics.Finding, error){
		entitytls.StageStoredUpgrade,
		entityinbounds.StageStoredUpgrade,
		entityoutbounds.StageStoredUpgrade,
		singboxconfig.StageBaseOptionsUpgrade,
		formats.StageStoredUpgrade,
	} {
		findings, err := stage(tx)
		if options.ReportFindings != nil {
			options.ReportFindings(findings)
		}
		if err != nil {
			return err
		}
	}
	projection, err := assembly.BuildCandidateProjectionFromDB(tx, "", true)
	if options.ReportFindings != nil {
		for _, findings := range [][]diagnostics.Finding{projection.RuleCompatibility, projection.DNSCompatibility, projection.HTTPCompatibility, projection.TLSCompatibility, projection.TransportCompatibility, projection.OptionsCompatibility} {
			options.ReportFindings(findings)
		}
	}
	if err != nil {
		return err
	}
	if err := singboxconfig.StageStoredBaseUpgrade(tx, projection.Config); err != nil {
		return err
	}
	if err := entityvalidation.ValidateStored(tx); err != nil {
		return err
	}
	// Reassemble persisted facts without historical inference. The result must
	// itself satisfy the complete current schema, rather than pass only because
	// another read-time upgrader silently repaired it.
	current, err := assembly.BuildCandidateProjectionFromDB(tx, "", false)
	if err != nil {
		return err
	}
	config := current.Config
	if err := singboxvalidation.ValidateConfigShape(config); err != nil {
		return err
	}
	if options.ProjectRuntimeFiles != nil {
		config, err = options.ProjectRuntimeFiles(tx, config)
		if err != nil {
			return err
		}
	}
	if err := singboxvalidation.ValidateConfig(config); err != nil {
		return diagnostics.FirstError([]diagnostics.Finding{{Kind: "config", Path: "config", Code: "UPGRADE_COMPLETE_BUILD_REJECTED", Severity: diagnostics.Error, Message: "The complete candidate is rejected by the pinned offline build. Correct current entity options and required owner files before retrying. Submitted values are excluded from this diagnostic.", MigrationOutcome: diagnostics.ManualRequired, OperatorActionRequired: true}})
	}
	return nil
}

func singBoxStateChecksum() string {
	// Include provider persistence shape and the bounded semantic contract.
	contract := []string{"solovey.core-schema/1.12", "sing-box/1.14.2", "owners:tls/inbounds/outbounds/base/subscriptions", "staged-whole-build-before-commit/v1", "legacy-hysteria2-cutoff/v1", "owner-files-ephemeral-validation/v1", "pinned-consumer-catalogue/v1"}
	t := reflect.TypeFor[model.TLSCertificateProvider]()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		contract = append(contract, f.Name, f.Type.String(), f.Tag.Get("gorm"))
	}
	encoded, _ := json.Marshal(contract)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
