package service

import (
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

type UserService struct {
	Runtime *Runtime
}

type DeleteUserResult struct {
	User              model.User
	DeletedTokenCount int64
}

func (s *UserService) runtime() *Runtime {
	if s != nil {
		return runtimeOrDefault(s.Runtime)
	}
	return DefaultRuntime()
}

func (s *UserService) GetFirstUser() (*model.User, error) {
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

func (s *UserService) UpdateFirstUser(username string, password string) error {
	if username == "" {
		return common.NewError("username can not be empty")
	} else if password == "" {
		return common.NewError("password can not be empty")
	}
	db := dbsqlite.DB()
	passwordHash, err := common.HashPassword(password)
	if err != nil {
		return err
	}
	user := &model.User{}
	err = db.Model(model.User{}).First(user).Error
	if dbsqlite.IsNotFound(err) {
		user.Username = username
		user.Password = passwordHash
		user.ForcePasswordReset = false
		return db.Model(model.User{}).Create(user).Error
	} else if err != nil {
		return err
	}
	user.Username = username
	user.Password = passwordHash
	user.ForcePasswordReset = false
	return db.Save(user).Error
}

func (s *UserService) Login(username string, password string, remoteIP string) (string, error) {
	user, needsMigration := s.CheckUser(username, password, remoteIP)
	if user == nil {
		return "", common.NewError("wrong user or password! IP: ", remoteIP)
	}
	if needsMigration {
		if err := s.updatePasswordHash(user, password); err != nil {
			logger.Warning("password migration failed:", err)
		}
	}
	// Flag a login from a new source IP (T1078) BEFORE RecordLogin overwrites the
	// previous last_logins, then record this login. Both are best-effort.
	s.detectNewLoginIP(user, remoteIP)
	s.RecordLogin(username, remoteIP)
	return user.Username, nil
}

// CheckUser is a pure query (Command-Query Separation): it validates the
// credentials and returns the user plus whether the stored hash needs
// migration. It performs NO writes — recording the login is RecordLogin's job.
func (s *UserService) CheckUser(username string, password string, remoteIP string) (*model.User, bool) {
	db := dbsqlite.DB()

	user := &model.User{}
	err := db.Model(model.User{}).
		Where("username = ?", username).
		First(user).
		Error
	if dbsqlite.IsNotFound(err) {
		// Equalize timing with the wrong-password path so a missing username is
		// not distinguishable by response latency (user enumeration).
		common.EqualizeLoginTiming(password)
		return nil, false
	} else if err != nil {
		logger.Warning("check user err:", err, " IP: ", remoteIP)
		return nil, false
	}
	ok, needsMigration := common.CheckPassword(user.Password, password)
	if !ok {
		return nil, false
	}
	return user, needsMigration
}

// RecordLogin persists the most recent login timestamp + IP for an admin. Kept
// out of CheckUser so the query stays pure; best-effort (logged, never blocks).
func (s *UserService) RecordLogin(username string, remoteIP string) {
	lastLoginTxt := time.Now().Format("2006-01-02 15:04:05") + " " + remoteIP
	if err := dbsqlite.DB().Model(model.User{}).
		Where("username = ?", username).
		Update("last_logins", &lastLoginTxt).Error; err != nil {
		logger.Warning("unable to log login data", err)
	}
}

// detectNewLoginIP records a warn audit when a successful login arrives from a
// source IP different from the admin's previous login (T1078). It reuses the
// existing last_logins value (no new storage) and must run BEFORE RecordLogin
// overwrites it. Best-effort.
func (s *UserService) detectNewLoginIP(user *model.User, remoteIP string) {
	prev := strings.TrimSpace(user.LastLogins)
	if prev == "" || remoteIP == "" {
		return
	}
	fields := strings.Fields(prev)
	prevIP := fields[len(fields)-1]
	if prevIP == "" || prevIP == remoteIP {
		return
	}
	_ = (&AuditService{}).Record(AuditEvent{
		Actor:    user.Username,
		Event:    "login_new_ip",
		Resource: "auth",
		Severity: AuditSeverityWarn,
		IP:       remoteIP,
		Details:  map[string]any{"previousIP": prevIP},
	})
}

func (s *UserService) GetUsers() (*[]model.User, error) {
	var users []model.User
	db := dbsqlite.DB()
	err := db.Model(model.User{}).Select("id,sort_order,username,last_logins").Order(entityorder.Clause).Scan(&users).Error
	if err != nil {
		return nil, err
	}
	return &users, nil
}

func (s *UserService) UserExists(username string) (bool, error) {
	if username == "" {
		return false, nil
	}
	var count int64
	err := dbsqlite.DB().Model(model.User{}).Where("username = ?", username).Count(&count).Error
	return count > 0, err
}

func (s *UserService) AddUser(actorUsername string, currentPass string, newUsername string, newPassword string) (*model.User, error) {
	newUsername = strings.TrimSpace(newUsername)
	if newUsername == "" {
		return nil, common.NewError("username can not be empty")
	}
	if newPassword == "" {
		return nil, common.NewError("password can not be empty")
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
			return common.NewError("user already exists")
		}
		passwordHash, err := common.HashPassword(newPassword)
		if err != nil {
			return err
		}
		sortOrder, err := entityorder.Next(tx, &model.User{})
		if err != nil {
			return err
		}
		created = model.User{
			SortOrder:          sortOrder,
			Username:           newUsername,
			Password:           passwordHash,
			ForcePasswordReset: false,
		}
		return tx.Create(&created).Error
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *UserService) DeleteUser(actorUsername string, currentPass string, targetID string) (DeleteUserResult, error) {
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
			return common.NewError("current admin can not be deleted")
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
func (s *UserService) ChangePass(username string, oldPass string, newUser string, newPass string) error {
	newUser = strings.TrimSpace(newUser)
	if newUser == "" {
		return common.NewError("username can not be empty")
	}
	if newPass == "" {
		return common.NewError("password can not be empty")
	}

	return dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		user := &model.User{}
		if err := tx.Model(model.User{}).Where("username = ?", username).First(user).Error; err != nil {
			return err
		}
		ok, _ := common.CheckPassword(user.Password, oldPass)
		if !ok {
			return common.NewError("wrong user or password")
		}
		if newUser != user.Username {
			var count int64
			if err := tx.Model(model.User{}).
				Where("username = ? AND id <> ?", newUser, user.Id).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return common.NewError("user already exists")
			}
		}
		passwordHash, err := common.HashPassword(newPass)
		if err != nil {
			return err
		}
		user.Username = newUser
		user.Password = passwordHash
		user.ForcePasswordReset = false
		return tx.Save(user).Error
	})
}

func (s *UserService) checkUserPassword(tx *gorm.DB, username string, password string) error {
	if username == "" || password == "" {
		return common.NewError("wrong user or password")
	}
	user := &model.User{}
	err := tx.Model(model.User{}).Where("username = ?", username).First(user).Error
	if dbsqlite.IsNotFound(err) {
		return common.NewError("wrong user or password")
	} else if err != nil {
		return err
	}
	ok, _ := common.CheckPassword(user.Password, password)
	if !ok {
		return common.NewError("wrong user or password")
	}
	return nil
}

func parseUserID(raw string) (uint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, common.NewError("user id can not be empty")
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, common.NewError("invalid user id")
	}
	return uint(id), nil
}

func (s *UserService) updatePasswordHash(user *model.User, password string) error {
	passwordHash, err := common.HashPassword(password)
	if err != nil {
		return err
	}
	return dbsqlite.DB().Model(model.User{}).Where("id = ?", user.Id).Updates(map[string]any{
		"password":             passwordHash,
		"force_password_reset": false,
	}).Error
}
