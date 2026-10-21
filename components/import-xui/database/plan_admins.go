//go:build !minimal

package importxui

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/components/import-xui/database/source"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	passwordutil "github.com/MalenkiySolovey/solovey-ui/util/password"

	"gorm.io/gorm"
)

func planAdmins(ctx context.Context, tx *gorm.DB, src *source.Database, plan *MigrationPlan, strategy Strategy, mode AdminMode) error {
	users, err := src.Users()
	if err != nil {
		return err
	}
	for _, user := range users {
		if err := checkContext(ctx); err != nil {
			return err
		}
		preview := map[string]any{
			"username": user.Username,
			"mode":     mode,
		}
		previewJSON, err := marshalJSON(preview)
		if err != nil {
			return err
		}
		conflict, err := recordExists(tx, &model.User{}, "username = ?", user.Username)
		if err != nil {
			return err
		}
		plan.Items = append(plan.Items, PlanItem{
			Kind:        KindAdmin,
			SrcID:       user.ID,
			SrcTag:      user.Username,
			DstTag:      user.Username,
			Action:      defaultAction(conflict, strategy),
			Conflict:    conflict,
			AdminMode:   string(mode),
			PreviewJSON: previewJSON,
		})
	}
	return nil
}

func (s *applyState) applyAdmins(ctx context.Context, tx *gorm.DB, src *source.Database) error {
	if !s.hasKind(KindAdmin) {
		return nil
	}
	users, err := src.Users()
	if err != nil {
		return err
	}
	for _, user := range users {
		if err := checkContext(ctx); err != nil {
			return err
		}
		item := s.item(KindAdmin, user.ID)
		if item.Action == ActionSkip {
			continue
		}
		username := firstSet(item.DstTag, user.Username)
		if err := identity.ValidateName(username); err != nil {
			return err
		}
		mode := AdminMode(firstSet(item.AdminMode, string(AdminModeNewPassword)))
		if err := mode.Validate(); err != nil {
			return err
		}
		switch mode {
		case AdminModeSkip:
			continue
		case AdminModeNewPassword:
			password, err := common.SecureRandom(24)
			if err != nil {
				return err
			}
			hash, err := passwordutil.Hash(ctx, password)
			if err != nil {
				return err
			}
			if err := upsertUserWithPassword(tx, username, hash, item.Action, false); err != nil {
				return err
			}
			s.report.GeneratedAdmins = append(s.report.GeneratedAdmins, GeneratedAdmin{Username: username, Password: password})
		case AdminModeResetRequired:
			hash, err := sourceAdminPasswordHash(ctx, user.Password)
			if err != nil {
				return err
			}
			if err := upsertUserResetRequired(tx, username, hash, item.Action); err != nil {
				return err
			}
		}
		s.progress("admins", username)
	}
	return nil
}

func firstSet(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sourceAdminPasswordHash(ctx context.Context, password string) (string, error) {
	if passwordutil.IsEncoded(password) {
		return password, nil
	}
	return passwordutil.Hash(ctx, password)
}

func upsertUserWithPassword(tx *gorm.DB, username, passwordHash, action string, forcePasswordReset bool) error {
	if action == ActionSkip || action == "" {
		return nil
	}
	return (&service.UserService{}).SaveImportedPanelAccount(tx, service.ImportedPanelAccount{
		Username: username, PasswordHash: passwordHash, ForcePasswordReset: forcePasswordReset,
	})
}

func upsertUserResetRequired(tx *gorm.DB, username, sourcePasswordHash, action string) error {
	if action == ActionSkip || action == "" {
		return nil
	}
	return (&service.UserService{}).SaveImportedPanelAccount(tx, service.ImportedPanelAccount{
		Username: username, PasswordHash: sourcePasswordHash, ForcePasswordReset: true, KeepExistingPassword: action == ActionMerge,
	})
}
