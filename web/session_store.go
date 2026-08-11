package web

import (
	"encoding/base32"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/service"
	ginsessions "github.com/gin-contrib/sessions"
	"github.com/gorilla/securecookie"
	gsessions "github.com/gorilla/sessions"
	"gorm.io/gorm"
)

type SQLiteSessionStore struct {
	db        *gorm.DB
	codecs    []securecookie.Codec
	optionsMu sync.RWMutex
	options   *gsessions.Options
	now       func() time.Time
	schemaMu  sync.Mutex
	schemaDB  *gorm.DB
}

type sqliteSessionRow struct {
	ID        string `gorm:"column:id"`
	Data      []byte `gorm:"column:data"`
	ExpiresAt int64  `gorm:"column:expires_at"`
}

var sessionIDEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func NewSQLiteSessionStore(db *gorm.DB, keyPairs ...[]byte) (*SQLiteSessionStore, error) {
	if db == nil {
		return nil, errors.New("sqlite session store requires an initialized database")
	}
	codecs := codecsFromHashKeys(keyPairs...)
	if len(codecs) == 0 {
		return nil, errors.New("sqlite session store requires at least one non-empty cookie key")
	}
	store := &SQLiteSessionStore{
		db:     db,
		codecs: codecs,
		options: &gsessions.Options{
			Path:     "/",
			MaxAge:   86400 * 30,
			SameSite: http.SameSiteLaxMode,
			HttpOnly: true,
			// Secure-by-default so any future code path that creates a session
			// without calling Options() still gets a Secure cookie. Production
			// login/CSRF flows override this via resolveCookieSecure().
			Secure: true,
		},
		now: time.Now,
	}
	if err := store.ensureSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteSessionStore) Options(options ginsessions.Options) {
	next := cloneSessionOptions(options.ToGorillaOptions())
	s.optionsMu.Lock()
	s.options = next
	s.optionsMu.Unlock()
}

func (s *SQLiteSessionStore) Get(r *http.Request, name string) (*gsessions.Session, error) {
	return gsessions.GetRegistry(r).Get(s, name)
}

func (s *SQLiteSessionStore) New(r *http.Request, name string) (*gsessions.Session, error) {
	session := gsessions.NewSession(s, name)
	session.Options = s.currentOptions()
	session.IsNew = true

	cookie, err := r.Cookie(name)
	if err != nil {
		return session, nil
	}
	if err := securecookie.DecodeMulti(name, cookie.Value, &session.ID, s.codecs...); err != nil {
		return session, err
	}
	loaded, err := s.load(session)
	if err != nil {
		return session, err
	}
	session.IsNew = !loaded
	if !loaded {
		session.ID = ""
		// load may have decoded a now-revoked or expired server-side session
		// before validation rejected it. Never leave those decoded values on
		// the fresh anonymous session returned to the request.
		session.Values = make(map[interface{}]interface{})
	}
	return session, nil
}

func (s *SQLiteSessionStore) Save(_ *http.Request, w http.ResponseWriter, session *gsessions.Session) error {
	if session.Options.MaxAge < 0 {
		if session.ID != "" {
			if err := s.erase(session.ID); err != nil {
				return err
			}
		}
		http.SetCookie(w, gsessions.NewCookie(session.Name(), "", session.Options))
		return nil
	}

	// Regenerate the session ID when the login flow requests it: erase the old
	// row and clear the ID so a fresh one is minted below. This defeats session
	// fixation — a pre-auth (CSRF) session cookie cannot survive authentication
	// under the same ID. The marker must not persist into the stored session data.
	replacedSessionID := ""
	replacedSessionRef := ""
	if _, regenerate := session.Values[service.SessionRegenerateKey]; regenerate {
		delete(session.Values, service.SessionRegenerateKey)
		if session.ID != "" {
			replacedSessionID = session.ID
			var previous model.SecuritySession
			if err := s.liveDB().Model(&model.SecuritySession{}).Where("session_id = ?", session.ID).First(&previous).Error; err == nil {
				replacedSessionRef = previous.Ref
			} else if !errors.Is(err, gorm.ErrRecordNotFound) && !isMissingSecuritySessionTable(err) {
				return err
			}
			session.ID = ""
		}
	}

	if session.ID == "" {
		session.ID = sessionIDEncoding.EncodeToString(securecookie.GenerateRandomKey(32))
	}
	if err := s.save(session, replacedSessionID); err != nil {
		return err
	}
	if replacedSessionID != "" {
		if replacedSessionRef != "" {
			realtime.CloseSession(replacedSessionRef, "session_replaced")
		}
	}
	encoded, err := securecookie.EncodeMulti(session.Name(), session.ID, s.codecs...)
	if err != nil {
		return err
	}
	http.SetCookie(w, gsessions.NewCookie(session.Name(), encoded, session.Options))
	return nil
}

func (s *SQLiteSessionStore) ensureSchema() error {
	liveDB := s.liveDB()
	if liveDB == nil {
		return errors.New("sqlite session store requires an initialized database")
	}

	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.schemaDB == liveDB {
		return nil
	}

	if err := liveDB.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	data BLOB NOT NULL,
	expires_at INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
		return err
	}
	if err := liveDB.Exec("CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)").Error; err != nil {
		return err
	}
	s.schemaDB = liveDB
	return nil
}

func (s *SQLiteSessionStore) save(session *gsessions.Session, replacedSessionID string) error {
	if err := s.ensureSchema(); err != nil {
		return err
	}
	encoded, err := securecookie.EncodeMulti(session.Name(), session.Values, s.codecs...)
	if err != nil {
		return err
	}
	expiresAt := int64(0)
	if session.Options.MaxAge > 0 {
		expiresAt = s.now().Add(time.Duration(session.Options.MaxAge) * time.Second).Unix()
	}
	return s.liveDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
INSERT INTO sessions(id, data, expires_at)
VALUES(?, ?, ?)
ON CONFLICT(id) DO UPDATE SET data = excluded.data, expires_at = excluded.expires_at
`, session.ID, []byte(encoded), expiresAt).Error; err != nil {
			return err
		}
		metadata, hasMetadata := securitySessionFromValues(session.ID, session.Values)
		if hasMetadata {
			if err := tx.Exec(`
INSERT INTO security_sessions(
	session_id, ref, user_id, username_snapshot, state, auth_state, assurance, last_mfa_at,
	lifetime_posture, session_generation_revision, credential_generation,
	mfa_generation, created_at, authenticated_at, last_seen_at, idle_expires_at,
	absolute_expires_at, remembered_expires_at, client_provenance, client_prefix,
	user_agent_hash, device_label, revoked_at, revoked_reason, replacement_ref
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(session_id) DO UPDATE SET
	ref=excluded.ref,
	user_id=excluded.user_id,
	username_snapshot=excluded.username_snapshot,
	state=excluded.state,
	auth_state=excluded.auth_state,
	assurance=excluded.assurance,
	last_mfa_at=excluded.last_mfa_at,
	lifetime_posture=excluded.lifetime_posture,
	session_generation_revision=excluded.session_generation_revision,
	credential_generation=excluded.credential_generation,
	mfa_generation=excluded.mfa_generation,
	last_seen_at=excluded.last_seen_at,
	idle_expires_at=excluded.idle_expires_at,
	absolute_expires_at=excluded.absolute_expires_at,
	remembered_expires_at=excluded.remembered_expires_at,
	client_provenance=excluded.client_provenance,
	client_prefix=excluded.client_prefix,
	user_agent_hash=excluded.user_agent_hash,
	device_label=excluded.device_label,
	revoked_at=0,
	revoked_reason='',
	replacement_ref=''
`,
				metadata.SessionID,
				metadata.Ref,
				metadata.UserID,
				metadata.UsernameSnapshot,
				metadata.State,
				metadata.AuthState,
				metadata.Assurance,
				metadata.LastMFAAt,
				metadata.LifetimePosture,
				metadata.SessionGenerationRevision,
				metadata.CredentialGeneration,
				metadata.MFAGeneration,
				metadata.CreatedAt,
				metadata.AuthenticatedAt,
				metadata.LastSeenAt,
				metadata.IdleExpiresAt,
				metadata.AbsoluteExpiresAt,
				metadata.RememberedExpiresAt,
				metadata.ClientProvenance,
				metadata.ClientPrefix,
				metadata.UserAgentHash,
				metadata.DeviceLabel,
				metadata.RevokedAt,
				metadata.RevokedReason,
				metadata.ReplacementRef,
			).Error; err != nil {
				return err
			}
		}
		if replacedSessionID == "" {
			return nil
		}
		replacementRef := ""
		if hasMetadata {
			replacementRef = metadata.Ref
		}
		if err := tx.Model(&model.SecuritySession{}).
			Where("session_id = ? AND revoked_at = 0", replacedSessionID).
			Updates(map[string]any{
				"state":           service.SessionStateRevoked,
				"revoked_at":      s.now().Unix(),
				"revoked_reason":  "session_replaced",
				"replacement_ref": replacementRef,
			}).Error; err != nil && !isMissingSecuritySessionTable(err) {
			return err
		}
		return tx.Exec("DELETE FROM sessions WHERE id = ?", replacedSessionID).Error
	})
}

func (s *SQLiteSessionStore) load(session *gsessions.Session) (bool, error) {
	if err := s.ensureSchema(); err != nil {
		return false, err
	}
	var row sqliteSessionRow
	err := s.liveDB().Table("sessions").Where("id = ?", session.ID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if row.ExpiresAt > 0 && row.ExpiresAt <= s.now().Unix() {
		if err := s.erase(row.ID); err != nil {
			return false, err
		}
		return false, nil
	}
	if err := securecookie.DecodeMulti(session.Name(), string(row.Data), &session.Values, s.codecs...); err != nil {
		return false, err
	}
	if valid, err := s.validateAndTouchSecuritySession(session); err != nil {
		return false, err
	} else if !valid {
		if err := s.erase(row.ID); err != nil {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

func (s *SQLiteSessionStore) erase(id string) error {
	if err := s.ensureSchema(); err != nil {
		return err
	}
	return s.liveDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SecuritySession{}).
			Where("session_id = ? AND revoked_at = 0", id).
			Updates(map[string]any{
				"state":          service.SessionStateRevoked,
				"revoked_at":     s.now().Unix(),
				"revoked_reason": "session_ended",
			}).Error; err != nil && !isMissingSecuritySessionTable(err) {
			return err
		}
		return tx.Exec("DELETE FROM sessions WHERE id = ?", id).Error
	})
}

func (s *SQLiteSessionStore) validateAndTouchSecuritySession(session *gsessions.Session) (bool, error) {
	ref, ok := session.Values[service.SessionRefKey].(string)
	if !ok || strings.TrimSpace(ref) == "" {
		return true, nil // explicit compatibility for sessions created before 1.8
	}
	userID, userOK := sessionValueUint64(session.Values[service.SessionUserIDKey])
	credentialGeneration, credentialOK := sessionValueUint64(session.Values[service.SessionCredentialGenerationKey])
	mfaGeneration, mfaOK := sessionValueUint64(session.Values[service.SessionMFAGenerationKey])
	if !userOK || !credentialOK || !mfaOK || userID == 0 {
		return false, nil
	}
	var metadata model.SecuritySession
	err := s.liveDB().Model(&model.SecuritySession{}).
		Where("session_id = ? AND ref = ?", session.ID, ref).
		First(&metadata).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var user model.User
	if err := s.liveDB().Model(&model.User{}).Where("id = ?", uint(userID)).First(&user).Error; err != nil {
		return false, err
	}
	now := s.now().Unix()
	if metadata.RevokedAt > 0 ||
		metadata.State == service.SessionStateRevoked ||
		metadata.UserID != uint(userID) ||
		metadata.CredentialGeneration != credentialGeneration ||
		metadata.MFAGeneration != mfaGeneration ||
		nonzeroSessionGeneration(user.CredentialGeneration) != credentialGeneration ||
		nonzeroSessionGeneration(user.MFAGeneration) != mfaGeneration ||
		(metadata.IdleExpiresAt > 0 && metadata.IdleExpiresAt <= now) ||
		(metadata.AbsoluteExpiresAt > 0 && metadata.AbsoluteExpiresAt <= now) ||
		(metadata.RememberedExpiresAt > 0 && metadata.RememberedExpiresAt <= now) {
		return false, nil
	}
	if now-metadata.LastSeenAt < int64(service.SessionLastSeenDebounce/time.Second) {
		return true, nil
	}
	nextIdle := int64(0)
	if metadata.IdleExpiresAt > 0 {
		idleWindow := metadata.IdleExpiresAt - metadata.LastSeenAt
		if idleWindow <= 0 {
			idleWindow = int64(service.DefaultSessionIdle / time.Second)
		}
		nextIdle = now + idleWindow
		if metadata.AbsoluteExpiresAt > 0 && nextIdle > metadata.AbsoluteExpiresAt {
			nextIdle = metadata.AbsoluteExpiresAt
		}
	}
	result := s.liveDB().Model(&model.SecuritySession{}).
		Where("session_id = ? AND last_seen_at = ? AND revoked_at = 0", session.ID, metadata.LastSeenAt).
		Updates(map[string]any{"last_seen_at": now, "idle_expires_at": nextIdle})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		session.Values[service.SessionLastSeenAtKey] = now
		session.Values[service.SessionIdleExpiresAtKey] = nextIdle
	}
	return true, nil
}

func securitySessionFromValues(sessionID string, values map[interface{}]interface{}) (model.SecuritySession, bool) {
	username, usernameOK := values[service.SessionLoginUserKey].(string)
	ref, refOK := values[service.SessionRefKey].(string)
	userID, userOK := sessionValueUint64(values[service.SessionUserIDKey])
	credentialGeneration, credentialOK := sessionValueUint64(values[service.SessionCredentialGenerationKey])
	mfaGeneration, mfaOK := sessionValueUint64(values[service.SessionMFAGenerationKey])
	if !usernameOK || !refOK || !userOK || !credentialOK || !mfaOK ||
		strings.TrimSpace(username) == "" || strings.TrimSpace(ref) == "" || userID == 0 {
		return model.SecuritySession{}, false
	}
	authState, _ := values[service.SessionAuthStateKey].(string)
	assurance, _ := values[service.SessionAssuranceKey].(string)
	lastMFAAt, _ := sessionValueInt64(values[service.SessionLastMFAAtKey])
	posture, _ := values[service.SessionLifetimePostureKey].(string)
	revision, _ := values[service.SessionGenerationRevisionKey].(string)
	clientProvenance, _ := values[service.SessionClientProvenanceKey].(string)
	clientPrefix, _ := values[service.SessionClientPrefixKey].(string)
	userAgentHash, _ := values[service.SessionUserAgentHashKey].(string)
	deviceLabel, _ := values[service.SessionDeviceLabelKey].(string)
	createdAt, _ := sessionValueInt64(values[service.SessionCreatedAtKey])
	authenticatedAt, _ := sessionValueInt64(values[service.SessionAuthenticatedAtKey])
	lastSeenAt, _ := sessionValueInt64(values[service.SessionLastSeenAtKey])
	idleExpiresAt, _ := sessionValueInt64(values[service.SessionIdleExpiresAtKey])
	absoluteExpiresAt, _ := sessionValueInt64(values[service.SessionAbsoluteExpiresAtKey])
	rememberedExpiresAt, _ := sessionValueInt64(values[service.SessionRememberedExpiresAtKey])
	state := service.SessionStateActive
	if authState != service.AuthStateAuthenticated {
		state = service.SessionStatePreAuth
	}
	return model.SecuritySession{
		SessionID:                 sessionID,
		Ref:                       ref,
		UserID:                    uint(userID),
		UsernameSnapshot:          username,
		State:                     state,
		AuthState:                 authState,
		Assurance:                 assurance,
		LastMFAAt:                 lastMFAAt,
		LifetimePosture:           posture,
		SessionGenerationRevision: revision,
		CredentialGeneration:      credentialGeneration,
		MFAGeneration:             mfaGeneration,
		CreatedAt:                 createdAt,
		AuthenticatedAt:           authenticatedAt,
		LastSeenAt:                lastSeenAt,
		IdleExpiresAt:             idleExpiresAt,
		AbsoluteExpiresAt:         absoluteExpiresAt,
		RememberedExpiresAt:       rememberedExpiresAt,
		ClientProvenance:          clientProvenance,
		ClientPrefix:              clientPrefix,
		UserAgentHash:             userAgentHash,
		DeviceLabel:               deviceLabel,
	}, true
}

func sessionValueUint64(value interface{}) (uint64, bool) {
	switch typed := value.(type) {
	case uint64:
		return typed, true
	case uint:
		return uint64(typed), true
	case int:
		if typed >= 0 {
			return uint64(typed), true
		}
	case int64:
		if typed >= 0 {
			return uint64(typed), true
		}
	}
	return 0, false
}

func sessionValueInt64(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case uint64:
		if typed <= uint64(^uint64(0)>>1) {
			return int64(typed), true
		}
	}
	return 0, false
}

func nonzeroSessionGeneration(value uint64) uint64 {
	if value == 0 {
		return 1
	}
	return value
}

func isMissingSecuritySessionTable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table")
}

func (s *SQLiteSessionStore) liveDB() *gorm.DB {
	if live := dbsqlite.DB(); live != nil {
		return live
	}
	return s.db
}

func (s *SQLiteSessionStore) currentOptions() *gsessions.Options {
	s.optionsMu.RLock()
	defer s.optionsMu.RUnlock()
	return cloneSessionOptions(s.options)
}

func cloneSessionOptions(options *gsessions.Options) *gsessions.Options {
	if options == nil {
		return nil
	}
	clone := *options
	return &clone
}

func codecsFromHashKeys(keys ...[]byte) []securecookie.Codec {
	codecs := make([]securecookie.Codec, 0, len(keys))
	for _, key := range keys {
		if len(key) == 0 {
			continue
		}
		codecs = append(codecs, securecookie.New(key, nil))
	}
	return codecs
}
