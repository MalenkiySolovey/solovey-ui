// Package accounts owns Panel administrator credentials and account transactions.
package accounts

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	backendaudit "github.com/MalenkiySolovey/solovey-ui/backend/audit"
	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	backendmfa "github.com/MalenkiySolovey/solovey-ui/backend/security/mfa"

	passwordutil "github.com/MalenkiySolovey/solovey-ui/backend/security/password"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"

	"gorm.io/gorm"
)

// Service uses the canonical Panel connection. Product supplies its existing
// ordering, diagnostics, audit and initial credential file operations.
// No Product runtime or second connection is retained here.
type Service struct {
	NextSortOrder           func(*gorm.DB) (int, error)
	ValidateImportName      func(string) error
	Warning                 func(...any)
	RecordAudit             func(backendaudit.Event) error
	RemoveInitialCredential func() error
}

func (s *Service) warning(args ...any) {
	if s != nil && s.Warning != nil {
		s.Warning(args...)
	}
}

func (s *Service) recordAudit(event backendaudit.Event) error {
	if s != nil && s.RecordAudit != nil {
		return s.RecordAudit(event)
	}
	return nil
}

func (s *Service) nextSortOrder(tx *gorm.DB) (int, error) {
	if s == nil || s.NextSortOrder == nil {
		return 0, errors.New("administrator ordering is unavailable")
	}
	return s.NextSortOrder(tx)
}

func (s *Service) removeInitialCredential() error {
	if s == nil || s.RemoveInitialCredential == nil {
		return errors.New("initial administrator credential cleanup is unavailable")
	}
	return s.RemoveInitialCredential()
}

type DeleteUserResult struct {
	User              model.User
	DeletedTokenCount int64
}

type PasswordTransitionResult struct {
	UserID                   uint
	Username                 string
	CredentialGeneration     uint64
	MFAGeneration            uint64
	InitialCredentialRemoved bool
}

func (s *Service) GetFirstUser() (*model.User, error) {
	db := dbsqlite.DB()

	user := &model.User{}
	err := db.Model(model.User{}).
		Order("id ASC").
		First(user).
		Error
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Service) UpdateFirstUser(username string, password string) error {
	if username == "" {
		return errors.New("username can not be empty")
	} else if err := passwordutil.ValidateNew(password); err != nil {
		return err
	}
	db := dbsqlite.DB()
	passwordHash, err := passwordutil.Hash(context.Background(), password)
	if err != nil {
		return err
	}
	user := &model.User{}
	err = db.Model(model.User{}).First(user).Error
	if dbsqlite.IsNotFound(err) {
		user.Username = username
		user.Password = passwordHash
		user.ForcePasswordReset = false
		user.PasswordPolicyVersion = passwordutil.PolicyVersion
		user.PasswordHashVersion = passwordutil.PolicyVersion
		user.CredentialGeneration = 1
		user.MFAGeneration = 1
		user.PasswordChangedAt = time.Now().Unix()
		return db.Model(model.User{}).Create(user).Error
	} else if err != nil {
		return err
	}
	user.Username = username
	user.Password = passwordHash
	user.ForcePasswordReset = false
	user.PasswordPolicyVersion = passwordutil.PolicyVersion
	user.PasswordHashVersion = passwordutil.PolicyVersion
	user.CredentialGeneration++
	if user.CredentialGeneration == 0 {
		user.CredentialGeneration = 1
	}
	if user.MFAGeneration == 0 {
		user.MFAGeneration = 1
	}
	user.PasswordChangedAt = time.Now().Unix()
	return db.Save(user).Error
}

// Authenticate verifies one password and transactionally upgrades compatible
// legacy hashes. A successful migration increments the credential generation;
// it never clears a forced-reset flag.
func (s *Service) Authenticate(ctx context.Context, username string, plaintext string, remoteIP string) (panelsecurity.AuthenticationResult, error) {
	db := dbsqlite.DB()
	var user model.User
	if err := db.Model(model.User{}).Where("username = ?", username).First(&user).Error; err != nil {
		if dbsqlite.IsNotFound(err) {
			_ = passwordutil.EqualizeUnknown(ctx, plaintext)
			return panelsecurity.AuthenticationResult{}, errors.New("wrong user or password")
		}
		s.warning("check user err:", err, " IP: ", remoteIP)
		return panelsecurity.AuthenticationResult{}, errors.New("wrong user or password")
	}

	valid, needsMigration, err := passwordutil.Verify(ctx, user.Password, plaintext)
	if err != nil {
		s.warning("password verification rejected:", err)
		return panelsecurity.AuthenticationResult{}, errors.New("wrong user or password")
	}
	if !valid {
		return panelsecurity.AuthenticationResult{}, errors.New("wrong user or password")
	}
	if needsMigration {
		upgraded, hashErr := passwordutil.Hash(ctx, plaintext)
		if hashErr != nil {
			return panelsecurity.AuthenticationResult{}, hashErr
		}
		err = db.Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&model.User{}).
				Where("id = ? AND password = ?", user.Id, user.Password).
				Updates(map[string]any{
					"password":              upgraded,
					"password_hash_version": passwordutil.PolicyVersion,
					"credential_generation": gorm.Expr("credential_generation + 1"),
				})
			if result.Error != nil {
				return result.Error
			}
			return tx.Model(&model.User{}).Where("id = ?", user.Id).First(&user).Error
		})
		if err != nil {
			return panelsecurity.AuthenticationResult{}, err
		}
	}

	s.detectNewLoginIP(&user, remoteIP)
	s.RecordLogin(user.Username, remoteIP)

	authState := panelsecurity.AuthStateAuthenticated
	assurance := panelsecurity.AssurancePassword
	if user.ForcePasswordReset {
		authState = panelsecurity.AuthStatePasswordReset
	}
	if authState == panelsecurity.AuthStateAuthenticated {
		var factor model.AdminMFAFactor
		err = db.Model(&model.AdminMFAFactor{}).
			Where("user_id = ? AND state IN ?", user.Id, backendmfa.ActiveMFAStates()).
			First(&factor).Error
		if err == nil {
			authState = panelsecurity.AuthStateMFAPending
		} else if !dbsqlite.IsNotFound(err) {
			return panelsecurity.AuthenticationResult{}, err
		}
	}
	return panelsecurity.NewAuthenticationResult(
		user.Id,
		user.Username,
		NormalizeGeneration(user.CredentialGeneration),
		NormalizeGeneration(user.MFAGeneration),
		authState,
		assurance,
	), nil
}

// CheckUser is a pure query (Command-Query Separation): it validates the
// credentials and returns the user plus whether the stored hash needs
// migration. It performs NO writes — recording the login is RecordLogin's job.
func (s *Service) CheckUser(username string, password string, remoteIP string) (*model.User, bool) {
	db := dbsqlite.DB()

	user := &model.User{}
	err := db.Model(model.User{}).
		Where("username = ?", username).
		First(user).
		Error
	if dbsqlite.IsNotFound(err) {
		// Equalize timing with the wrong-password path so a missing username is
		// not distinguishable by response latency (user enumeration).
		if err := passwordutil.EqualizeUnknown(context.Background(), password); err != nil {
			s.warning("equalize unknown login timing: ", err)
		}
		return nil, false
	} else if err != nil {
		s.warning("check user err:", err, " IP: ", remoteIP)
		return nil, false
	}
	ok, needsMigration, verifyErr := passwordutil.Verify(context.Background(), user.Password, password)
	if verifyErr != nil {
		s.warning("verify user password: ", verifyErr)
		return nil, false
	}
	if !ok {
		return nil, false
	}
	return user, needsMigration
}

// RecordLogin persists the most recent login timestamp + IP for an admin. Kept
// out of CheckUser so the query stays pure; best-effort (logged, never blocks).
func (s *Service) RecordLogin(username string, remoteIP string) {
	lastLoginTxt := time.Now().Format("2006-01-02 15:04:05") + " " + remoteIP
	if err := dbsqlite.DB().Model(model.User{}).
		Where("username = ?", username).
		Update("last_logins", &lastLoginTxt).Error; err != nil {
		s.warning("unable to log login data", err)
	}
}

// detectNewLoginIP records a warn audit when a successful login arrives from a
// source IP different from the admin's previous login (T1078). It reuses the
// existing last_logins value (no new storage) and must run BEFORE RecordLogin
// overwrites it. Best-effort.
func (s *Service) detectNewLoginIP(user *model.User, remoteIP string) {
	prev := strings.TrimSpace(user.LastLogins)
	if prev == "" || remoteIP == "" {
		return
	}
	fields := strings.Fields(prev)
	prevIP := fields[len(fields)-1]
	if prevIP == "" || prevIP == remoteIP {
		return
	}
	_ = s.recordAudit(backendaudit.Event{
		Actor:    user.Username,
		Event:    "login_new_ip",
		Resource: "auth",
		Severity: backendaudit.SeverityWarn,
		IP:       remoteIP,
		Details:  map[string]any{"previousIP": prevIP},
	})
}

func (s *Service) GetUsers() (*[]model.User, error) {
	var users []model.User
	db := dbsqlite.DB()
	err := db.Model(model.User{}).Select("id,sort_order,username,last_logins").Order("sort_order ASC, id ASC").Scan(&users).Error
	if err != nil {
		return nil, err
	}
	return &users, nil
}

func (s *Service) AddUser(actorUsername string, currentPass string, newUsername string, newPassword string) (*model.User, error) {
	newUsername = strings.TrimSpace(newUsername)
	if newUsername == "" {
		return nil, errors.New("username can not be empty")
	}
	if newPassword == "" {
		return nil, errors.New("password can not be empty")
	}
	if err := passwordutil.ValidateNew(newPassword); err != nil {
		return nil, err
	}

	var created model.User
	err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.checkUserPassword(tx, actorUsername, currentPass); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(model.User{}).Where("username = ?", newUsername).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("user already exists")
		}
		passwordHash, err := passwordutil.Hash(context.Background(), newPassword)
		if err != nil {
			return err
		}
		sortOrder, err := s.nextSortOrder(tx)
		if err != nil {
			return err
		}
		created = model.User{
			SortOrder:             sortOrder,
			Username:              newUsername,
			Password:              passwordHash,
			ForcePasswordReset:    false,
			PasswordPolicyVersion: passwordutil.PolicyVersion,
			PasswordHashVersion:   passwordutil.PolicyVersion,
			CredentialGeneration:  1,
			MFAGeneration:         1,
			PasswordChangedAt:     time.Now().Unix(),
		}
		return tx.Create(&created).Error
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *Service) DeleteUser(actorUsername string, currentPass string, targetID string) (DeleteUserResult, error) {
	var result DeleteUserResult
	id, err := parseUserID(targetID)
	if err != nil {
		return result, err
	}
	err = dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.checkUserPassword(tx, actorUsername, currentPass); err != nil {
			return err
		}
		var target model.User
		if err := tx.Model(model.User{}).Where("id = ?", id).First(&target).Error; err != nil {
			return err
		}
		if target.Username == actorUsername {
			return errors.New("current admin can not be deleted")
		}
		tokenDelete := tx.Where("user_id = ?", target.Id).Delete(&model.Tokens{})
		if tokenDelete.Error != nil {
			return tokenDelete.Error
		}
		if err := tx.Delete(&target).Error; err != nil {
			return err
		}
		result.User = target
		result.DeletedTokenCount = tokenDelete.RowsAffected
		return nil
	})
	return result, err
}

// ChangePass updates the credentials of the user identified by username. The
// caller passes the AUTHENTICATED session user's name (never a client-supplied
// id), so an admin can only change their own account, not another admin's.
func (s *Service) ChangePass(username string, oldPass string, newUser string, newPass string) error {
	newUser = strings.TrimSpace(newUser)
	if newUser == "" {
		return errors.New("username can not be empty")
	}
	if newPass == "" {
		return errors.New("password can not be empty")
	}
	if err := passwordutil.ValidateNew(newPass); err != nil {
		return err
	}

	return dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		user := &model.User{}
		if err := tx.Model(model.User{}).Where("username = ?", username).First(user).Error; err != nil {
			return err
		}
		ok, _, err := passwordutil.Verify(context.Background(), user.Password, oldPass)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("wrong user or password")
		}
		if newUser != user.Username {
			var count int64
			if err := tx.Model(model.User{}).
				Where("username = ? AND id <> ?", newUser, user.Id).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errors.New("user already exists")
			}
		}
		passwordHash, err := passwordutil.Hash(context.Background(), newPass)
		if err != nil {
			return err
		}
		user.Username = newUser
		user.Password = passwordHash
		user.ForcePasswordReset = false
		user.PasswordPolicyVersion = passwordutil.PolicyVersion
		user.PasswordHashVersion = passwordutil.PolicyVersion
		user.CredentialGeneration = NormalizeGeneration(user.CredentialGeneration) + 1
		user.PasswordChangedAt = time.Now().Unix()
		return tx.Save(user).Error
	})
}

// CompletePasswordTransition is the only path that clears a forced reset. The
// credential row is committed before the one-time initial credential file is
// removed, so an interruption can never leave neither credential usable.
func (s *Service) CompletePasswordTransition(ctx context.Context, userID uint, oldPass, newUsername, newPass string) (PasswordTransitionResult, error) {
	newUsername = strings.TrimSpace(newUsername)
	if userID == 0 || newUsername == "" {
		return PasswordTransitionResult{}, errors.New("username can not be empty")
	}
	if err := passwordutil.ValidateNew(newPass); err != nil {
		return PasswordTransitionResult{}, err
	}
	db := dbsqlite.DB().WithContext(ctx)
	var user model.User
	if err := db.Model(&model.User{}).Where("id = ?", userID).First(&user).Error; err != nil {
		return PasswordTransitionResult{}, err
	}
	valid, _, verifyErr := passwordutil.Verify(ctx, user.Password, oldPass)
	if verifyErr != nil || !valid {
		return PasswordTransitionResult{}, errors.New("wrong user or password")
	}
	newHash, err := passwordutil.Hash(ctx, newPass)
	if err != nil {
		return PasswordTransitionResult{}, err
	}
	nextCredentialGeneration := NormalizeGeneration(user.CredentialGeneration) + 1
	updates := map[string]any{
		"username":                newUsername,
		"password":                newHash,
		"force_password_reset":    false,
		"password_policy_version": passwordutil.PolicyVersion,
		"password_hash_version":   passwordutil.PolicyVersion,
		"credential_generation":   nextCredentialGeneration,
		"password_changed_at":     time.Now().Unix(),
	}
	// Keep the expensive password verification outside a SQLite read
	// transaction. A deferred read transaction cannot safely upgrade after a
	// concurrent WAL writer commits and returns SQLITE_BUSY without honoring the
	// busy timeout. This single conditional UPDATE is the atomic commit point:
	// it serializes writers, rejects duplicate usernames, and CAS-binds the
	// credential plus both security generations observed during verification.
	update := db.Model(&model.User{}).
		Where(`id = ? AND password = ? AND credential_generation = ? AND mfa_generation = ? AND NOT EXISTS (
			SELECT 1 FROM users AS other WHERE other.username = ? AND other.id <> ?
		)`, user.Id, user.Password, user.CredentialGeneration, user.MFAGeneration, newUsername, user.Id).
		Updates(updates)
	if update.Error != nil {
		return PasswordTransitionResult{}, update.Error
	}
	if update.RowsAffected != 1 {
		if newUsername != user.Username {
			var count int64
			if err := db.Model(&model.User{}).
				Where("username = ? AND id <> ?", newUsername, user.Id).
				Count(&count).Error; err != nil {
				return PasswordTransitionResult{}, err
			}
			if count > 0 {
				return PasswordTransitionResult{}, errors.New("user already exists")
			}
		}
		return PasswordTransitionResult{}, errors.New("credential changed")
	}
	result := PasswordTransitionResult{
		UserID:               user.Id,
		Username:             newUsername,
		CredentialGeneration: nextCredentialGeneration,
		MFAGeneration:        NormalizeGeneration(user.MFAGeneration),
	}
	if err := s.removeInitialCredential(); err != nil {
		s.warning("unable to remove acknowledged initial admin password file:", err)
	} else {
		result.InitialCredentialRemoved = true
	}
	return result, nil
}

// ChangeCredentialAfterStepUp changes the authenticated administrator's own
// credential after the HTTP boundary has consumed a purpose-bound step-up
// grant. It deliberately does not accept a target user ID from the client and
// preserves the enrolled MFA factor.
func (s *Service) ChangeCredentialAfterStepUp(ctx context.Context, userID uint, newUsername, newPass string) (PasswordTransitionResult, error) {
	newUsername = strings.TrimSpace(newUsername)
	if userID == 0 || newUsername == "" {
		return PasswordTransitionResult{}, errors.New("username can not be empty")
	}
	if err := passwordutil.ValidateNew(newPass); err != nil {
		return PasswordTransitionResult{}, err
	}
	newHash, err := passwordutil.Hash(ctx, newPass)
	if err != nil {
		return PasswordTransitionResult{}, err
	}
	var result PasswordTransitionResult
	err = dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Model(&model.User{}).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		if newUsername != user.Username {
			var count int64
			if err := tx.Model(&model.User{}).
				Where("username = ? AND id <> ?", newUsername, user.Id).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errors.New("user already exists")
			}
		}
		nextCredentialGeneration := NormalizeGeneration(user.CredentialGeneration) + 1
		update := tx.Model(&model.User{}).Where(
			"id = ? AND credential_generation = ?",
			user.Id, user.CredentialGeneration,
		).Updates(map[string]any{
			"username":                newUsername,
			"password":                newHash,
			"force_password_reset":    false,
			"password_policy_version": passwordutil.PolicyVersion,
			"password_hash_version":   passwordutil.PolicyVersion,
			"credential_generation":   nextCredentialGeneration,
			"password_changed_at":     time.Now().Unix(),
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errors.New("credential changed")
		}
		if err := tx.Where("user_id = ?", user.Id).Delete(&model.StepUpGrant{}).Error; err != nil {
			return err
		}
		result = PasswordTransitionResult{
			UserID:               user.Id,
			Username:             newUsername,
			CredentialGeneration: nextCredentialGeneration,
			MFAGeneration:        NormalizeGeneration(user.MFAGeneration),
		}
		return nil
	})
	return result, err
}

// CompleteRecoveryTransition is authorized only by a short-lived
// recovery-code session. It replaces the credential and disables the
// compromised/lost factor in one transaction before a normal session can be
// issued.
func (s *Service) CompleteRecoveryTransition(ctx context.Context, userID uint, newUsername, newPass string) (PasswordTransitionResult, error) {
	newUsername = strings.TrimSpace(newUsername)
	if userID == 0 || newUsername == "" {
		return PasswordTransitionResult{}, errors.New("username can not be empty")
	}
	if err := passwordutil.ValidateNew(newPass); err != nil {
		return PasswordTransitionResult{}, err
	}
	newHash, err := passwordutil.Hash(ctx, newPass)
	if err != nil {
		return PasswordTransitionResult{}, err
	}
	var result PasswordTransitionResult
	err = dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Model(&model.User{}).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		if newUsername != user.Username {
			var count int64
			if err := tx.Model(&model.User{}).
				Where("username = ? AND id <> ?", newUsername, user.Id).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errors.New("user already exists")
			}
		}
		nextCredentialGeneration := NormalizeGeneration(user.CredentialGeneration) + 1
		nextMFAGeneration := NormalizeGeneration(user.MFAGeneration) + 1
		update := tx.Model(&model.User{}).Where(
			"id = ? AND credential_generation = ? AND mfa_generation = ?",
			user.Id, user.CredentialGeneration, user.MFAGeneration,
		).Updates(map[string]any{
			"username":                newUsername,
			"password":                newHash,
			"force_password_reset":    false,
			"password_policy_version": passwordutil.PolicyVersion,
			"password_hash_version":   passwordutil.PolicyVersion,
			"credential_generation":   nextCredentialGeneration,
			"mfa_generation":          nextMFAGeneration,
			"password_changed_at":     time.Now().Unix(),
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errors.New("security state changed")
		}
		if err := tx.Where("user_id = ?", user.Id).Delete(&model.AdminRecoveryCode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.Id).Delete(&model.AdminMFAFactor{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.Id).Delete(&model.StepUpGrant{}).Error; err != nil {
			return err
		}
		result = PasswordTransitionResult{
			UserID:               user.Id,
			Username:             newUsername,
			CredentialGeneration: nextCredentialGeneration,
			MFAGeneration:        nextMFAGeneration,
		}
		return nil
	})
	return result, err
}

func (s *Service) checkUserPassword(tx *gorm.DB, username string, password string) error {
	if username == "" || password == "" {
		return errors.New("wrong user or password")
	}
	user := &model.User{}
	err := tx.Model(model.User{}).Where("username = ?", username).First(user).Error
	if dbsqlite.IsNotFound(err) {
		return errors.New("wrong user or password")
	} else if err != nil {
		return err
	}
	ok, _, err := passwordutil.Verify(context.Background(), user.Password, password)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("wrong user or password")
	}
	return nil
}

func parseUserID(raw string) (uint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, errors.New("user id can not be empty")
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, errors.New("invalid user id")
	}
	return uint(id), nil
}

// NormalizeGeneration preserves the existing nonzero administrator revision convention.
func NormalizeGeneration(value uint64) uint64 {
	if value == 0 {
		return 1
	}
	return value
}
