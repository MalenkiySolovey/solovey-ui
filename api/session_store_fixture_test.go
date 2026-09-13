package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	ginsessions "github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	gsessions "github.com/gorilla/sessions"
	"gorm.io/gorm"
)

// apiTestSessionStore keeps lightweight cookie transport for broad API unit
// fixtures while mirroring the same mandatory modern SecuritySession metadata
// consumed by production API authentication. Shipped-store behavior is covered
// separately by the external SQLiteSessionStore API integration test.
type apiTestSessionStore struct {
	inner ginsessions.Store
}

func newAPITestSessionStore(tb testing.TB) ginsessions.Store {
	tb.Helper()
	return &apiTestSessionStore{inner: cookie.NewStore([]byte("test-secret"))}
}

func (s *apiTestSessionStore) Options(options ginsessions.Options) {
	s.inner.Options(options)
}

func (s *apiTestSessionStore) Get(r *http.Request, name string) (*gsessions.Session, error) {
	return gsessions.GetRegistry(r).Get(s, name)
}

func (s *apiTestSessionStore) New(r *http.Request, name string) (*gsessions.Session, error) {
	session := gsessions.NewSession(s, name)
	inner, ok := s.inner.(interface {
		New(*http.Request, string) (*gsessions.Session, error)
	})
	if !ok {
		return session, gorm.ErrInvalidDB
	}
	decoded, err := inner.New(r, name)
	session.Options = decoded.Options
	session.Values = decoded.Values
	session.IsNew = decoded.IsNew
	return session, err
}

func (s *apiTestSessionStore) Save(r *http.Request, w http.ResponseWriter, session *gsessions.Session) error {
	if err := s.inner.Save(r, w, session); err != nil {
		return err
	}
	return mirrorAPITestSecuritySession(session)
}

func mirrorAPITestSecuritySession(session *gsessions.Session) error {
	if session == nil || session.Options.MaxAge < 0 {
		return nil
	}
	username, usernameOK := session.Values[service.SessionLoginUserKey].(string)
	ref, refOK := session.Values[service.SessionRefKey].(string)
	userID, userOK := sessionUint64(session.Values[service.SessionUserIDKey])
	credentialGeneration, credentialOK := sessionUint64(session.Values[service.SessionCredentialGenerationKey])
	mfaGeneration, mfaOK := sessionUint64(session.Values[service.SessionMFAGenerationKey])
	if !usernameOK || !refOK || !userOK || !credentialOK || !mfaOK || strings.TrimSpace(username) == "" || strings.TrimSpace(ref) == "" || userID == 0 {
		return nil
	}
	db := dbsqlite.DB()
	if db == nil {
		return gorm.ErrInvalidDB
	}
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		data BLOB NOT NULL,
		expires_at INTEGER NOT NULL DEFAULT 0
	)`).Error; err != nil {
		return err
	}
	int64Value := func(key string) int64 {
		value, _ := sessionUint64(session.Values[key])
		return int64(value)
	}
	stringValue := func(key string) string {
		value, _ := session.Values[key].(string)
		return value
	}
	authState := stringValue(service.SessionAuthStateKey)
	state := service.SessionStateActive
	if authState != service.AuthStateAuthenticated {
		state = service.SessionStatePreAuth
	}
	row := model.SecuritySession{
		SessionID: "cookie-fixture:" + ref, Ref: ref, UserID: uint(userID), UsernameSnapshot: username,
		State: state, AuthState: authState, Assurance: stringValue(service.SessionAssuranceKey),
		LastMFAAt: int64Value(service.SessionLastMFAAtKey), LifetimePosture: stringValue(service.SessionLifetimePostureKey),
		SessionGenerationRevision: stringValue(service.SessionGenerationRevisionKey),
		CredentialGeneration:      credentialGeneration, MFAGeneration: mfaGeneration,
		CreatedAt: int64Value(service.SessionCreatedAtKey), AuthenticatedAt: int64Value(service.SessionAuthenticatedAtKey),
		LastSeenAt: int64Value(service.SessionLastSeenAtKey), IdleExpiresAt: int64Value(service.SessionIdleExpiresAtKey),
		AbsoluteExpiresAt: int64Value(service.SessionAbsoluteExpiresAtKey), RememberedExpiresAt: int64Value(service.SessionRememberedExpiresAtKey),
		ClientProvenance: stringValue(service.SessionClientProvenanceKey), ClientPrefix: stringValue(service.SessionClientPrefixKey),
		UserAgentHash: stringValue(service.SessionUserAgentHashKey), DeviceLabel: stringValue(service.SessionDeviceLabelKey),
	}
	return db.Save(&row).Error
}

func TestAPITestSessionStoreMirrorsModernMetadata(t *testing.T) {
	settingService := initSessionTestDB(t)
	ginRouter := newSessionTestRouter(t, settingService)
	login := performSessionRequest(ginRouter, "/login")
	if login.Code != http.StatusNoContent {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	var row model.SecuritySession
	if err := dbsqlite.DB().Order("created_at DESC").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&service.SecuritySessionService{}).Validate(row.Ref, row.UserID, row.CredentialGeneration, row.MFAGeneration); err != nil {
		t.Fatalf("mirrored metadata rejected: %#v err=%v", row, err)
	}
	if response := performSessionRequest(ginRouter, "/protected", login.Result().Cookies()...); response.Code != http.StatusNoContent {
		t.Fatalf("mirrored session status=%d row=%#v", response.Code, row)
	}
}
