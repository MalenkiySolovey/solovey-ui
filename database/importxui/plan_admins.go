package importxui

import (
	"context"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

func planAdmins(ctx context.Context, tx *gorm.DB, src *sourceDB, plan *MigrationPlan, strategy Strategy, mode AdminMode) error {
	users, err := src.users()
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

func (s *applyState) applyAdmins(ctx context.Context, tx *gorm.DB, src *sourceDB) error {
	if !s.hasKind(KindAdmin) {
		return nil
	}
	users, err := src.users()
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
		username := firstNonEmpty(item.DstTag, user.Username)
		mode := AdminMode(firstNonEmpty(item.AdminMode, string(AdminModeNewPassword)))
		if err := mode.Validate(); err != nil {
			return err
		}
		switch mode {
		case AdminModeSkip:
			continue
		case AdminModeNewPassword:
			password := deterministicSeq(username+":admin:"+strconv.FormatInt(time.Now().UnixNano(), 10), 16)
			hash, err := common.HashPassword(password)
			if err != nil {
				return err
			}
			if err := upsertUserWithPassword(tx, username, hash, item.Action, false); err != nil {
				return err
			}
			s.report.GeneratedAdmins = append(s.report.GeneratedAdmins, GeneratedAdmin{Username: username, Password: password})
		case AdminModeResetRequired:
			hash, err := sourceAdminPasswordHash(user.Password)
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

func sourceAdminPasswordHash(password string) (string, error) {
	if common.IsPasswordHash(password) {
		return password, nil
	}
	return common.HashPassword(password)
}

func upsertUserWithPassword(tx *gorm.DB, username string, passwordHash string, action string, forcePasswordReset bool) error {
	var user model.User
	err := tx.Where("username = ?", username).First(&user).Error
	if err != nil && !database.IsNotFound(err) {
		return err
	}
	if database.IsNotFound(err) {
		return tx.Create(&model.User{Username: username, Password: passwordHash, ForcePasswordReset: forcePasswordReset}).Error
	}
	if action == ActionSkip || action == "" {
		return nil
	}
	return tx.Model(&user).Updates(map[string]any{
		"password":             passwordHash,
		"force_password_reset": forcePasswordReset,
	}).Error
}

func upsertUserResetRequired(tx *gorm.DB, username string, sourcePasswordHash string, action string) error {
	var user model.User
	err := tx.Where("username = ?", username).First(&user).Error
	if err != nil && !database.IsNotFound(err) {
		return err
	}
	if database.IsNotFound(err) {
		if action == ActionSkip || action == "" {
			return nil
		}
		return tx.Create(&model.User{Username: username, Password: sourcePasswordHash, ForcePasswordReset: true}).Error
	}
	if action == ActionSkip || action == "" {
		return nil
	}
	updates := map[string]any{"force_password_reset": true}
	if action != ActionMerge {
		updates["password"] = sourcePasswordHash
	}
	return tx.Model(&user).Updates(updates).Error
}
