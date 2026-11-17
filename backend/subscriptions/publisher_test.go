package subscriptions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type publishingSettings struct {
	enabled, required bool
	policyError       error
}

func (*publishingSettings) GetSubShowInfo() (bool, error)         { return false, nil }
func (*publishingSettings) GetSubNameInRemark() (bool, error)     { return false, nil }
func (*publishingSettings) GetSubUpdates() (int, error)           { return 12, nil }
func (*publishingSettings) GetSubTitle() (string, error)          { return "title", nil }
func (*publishingSettings) GetSubSupportUrl() (string, error)     { return "", nil }
func (*publishingSettings) GetSubProfileUrl() (string, error)     { return "", nil }
func (*publishingSettings) GetSubAnnounce() (string, error)       { return "", nil }
func (*publishingSettings) GetSubEncode() (bool, error)           { return false, nil }
func (s *publishingSettings) GetSubSecretRequired() (bool, error) { return s.required, s.policyError }
func (s *publishingSettings) GetSubLinkEnable() (bool, error)     { return s.enabled, nil }
func (s *publishingSettings) GetSubJsonEnable() (bool, error)     { return s.enabled, nil }
func (s *publishingSettings) GetSubClashEnable() (bool, error)    { return s.enabled, nil }
func (s *publishingSettings) GetSubXrayEnable() (bool, error)     { return s.enabled, nil }

type publishingProjection struct {
	calls, renders int
	version        uint64
	borrowed       []byte
	renderError    error
}

func (p *publishingProjection) PrivateLinks(c *model.Client) (*model.Client, error) {
	p.calls++
	return c, nil
}
func (p *publishingProjection) LoadDelivery(c *model.Client) (*model.Client, []*model.Inbound, error) {
	p.calls++
	copy := *c
	p.borrowed = []byte("borrowed credential")
	copy.Config = p.borrowed
	return &copy, nil, nil
}
func (p *publishingProjection) ResolveLinks(json.RawMessage, string) []string {
	p.calls++
	return []string{"link"}
}
func (p *publishingProjection) render() (string, error) {
	p.calls++
	p.renders++
	return "rendered", p.renderError
}
func (p *publishingProjection) RenderJSON(*model.Client, []*model.Inbound) (string, error) {
	return p.render()
}
func (p *publishingProjection) RenderClash(*model.Client, []*model.Inbound) (string, error) {
	return p.render()
}
func (p *publishingProjection) RenderXray(*model.Client, []*model.Inbound) (string, error) {
	return p.render()
}
func (p *publishingProjection) ContributorsVersion() uint64 { return p.version }

func publishingDB(t *testing.T) *gorm.DB {
	t.Helper()
	ClearSubscriptionOutputCache()
	err := dbsqlite.InitializeContext(context.Background(), filepath.Join(t.TempDir(), "panel.db"), dbsqlite.Bootstrap{
		BeforeOpen: func() error { return nil }, AfterOpen: func(db *gorm.DB) error { return db.AutoMigrate(&model.Client{}) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ClearSubscriptionOutputCache()
		if err := dbsqlite.Close(); err != nil {
			t.Error(err)
		}
	})
	return dbsqlite.DB()
}
func publishingClient(t *testing.T, db *gorm.DB, enabled bool, secret string) model.Client {
	t.Helper()
	c := model.Client{Enable: enabled, Name: "user", SubSecret: secret, Config: json.RawMessage(`{"stored":"envelope"}`), Links: json.RawMessage(`[]`), Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	return c
}

func TestUnauthorizedSubscriptionNeverReachesDeliveryProjection(t *testing.T) {
	for _, state := range []string{"missing", "disabled", "ambiguous"} {
		t.Run(state, func(t *testing.T) {
			db := publishingDB(t)
			switch state {
			case "disabled":
				publishingClient(t, db, false, "secret")
			case "ambiguous":
				if db.Migrator().HasIndex(&model.Client{}, "idx_clients_sub_secret") {
					if err := db.Migrator().DropIndex(&model.Client{}, "idx_clients_sub_secret"); err != nil {
						t.Fatal(err)
					}
				}
				publishingClient(t, db, true, "secret")
				publishingClient(t, db, true, "secret")
			}
			projection := &publishingProjection{}
			p := Publisher{Settings: &publishingSettings{enabled: true, required: true}, Projection: projection}
			for _, get := range []func(string) (*string, []string, error){p.GetSubs, p.GetJSON, p.GetClash, p.GetXray} {
				body, _, err := get("secret")
				if err == nil || body != nil {
					t.Fatalf("unauthorized subscription returned output: %v", err)
				}
				if state != "ambiguous" && !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatalf("lookup error changed: %v", err)
				}
				if state == "ambiguous" && errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatal("ambiguous identity was hidden as absent")
				}
			}
			if projection.calls != 0 {
				t.Fatalf("unauthorized provider effects: %d", projection.calls)
			}
		})
	}
}

func TestLegacyPolicyFailureCannotBackfillSecretOrProjectCredentials(t *testing.T) {
	db := publishingDB(t)
	c := publishingClient(t, db, true, "")
	failure := errors.New("policy storage failure")
	projection := &publishingProjection{}
	p := Publisher{Settings: &publishingSettings{enabled: true, policyError: failure}, Projection: projection}
	if _, _, err := p.GetSubs(c.Name); !errors.Is(err, failure) {
		t.Fatalf("policy error = %v", err)
	}
	var stored model.Client
	if err := db.First(&stored, c.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SubSecret != "" || projection.calls != 0 {
		t.Fatal("policy rejection changed identity or borrowed credentials")
	}
}

func TestFormatGatesRejectPreviouslyCachedOutput(t *testing.T) {
	ClearSubscriptionOutputCache()
	t.Cleanup(ClearSubscriptionOutputCache)
	for _, key := range []string{"base:secret", "json:secret:clientHooks=0", "clash:secret:clientHooks=0", "xray:secret:clientHooks=0"} {
		subscriptionCacheSet(key, "cached credential", nil, time.Now())
	}
	projection := &publishingProjection{}
	p := Publisher{Settings: &publishingSettings{}, Projection: projection}
	for _, get := range []func(string) (*string, []string, error){p.GetSubs, p.GetJSON, p.GetClash, p.GetXray} {
		if body, _, err := get("secret"); err == nil || body != nil {
			t.Fatal("disabled format returned cached output")
		}
	}
	if projection.calls != 0 {
		t.Fatal("disabled format reached projection")
	}
}

func TestRenderedCredentialCopiesAreWipedOnSuccessAndFailure(t *testing.T) {
	for _, format := range []string{"json", "clash", "xray"} {
		for _, fail := range []bool{false, true} {
			t.Run(format+map[bool]string{true: "-failure", false: "-success"}[fail], func(t *testing.T) {
				db := publishingDB(t)
				c := publishingClient(t, db, true, "secret")
				projection := &publishingProjection{}
				if fail {
					projection.renderError = errors.New("render rejected")
				}
				p := Publisher{Settings: &publishingSettings{enabled: true, required: true}, Projection: projection}
				get := map[string]func(string) (*string, []string, error){"json": p.GetJSON, "clash": p.GetClash, "xray": p.GetXray}[format]
				body, _, err := get("secret")
				if fail {
					if !errors.Is(err, projection.renderError) || body != nil {
						t.Fatalf("render failure = %v", err)
					}
				} else if err != nil || body == nil || *body != "rendered" {
					t.Fatalf("render output = %v", err)
				}
				if len(projection.borrowed) == 0 || !bytes.Equal(projection.borrowed, make([]byte, len(projection.borrowed))) {
					t.Fatal("borrowed credential remained readable")
				}
				var stored model.Client
				if err := db.First(&stored, c.Id).Error; err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(stored.Config, c.Config) {
					t.Fatal("delivery wiped or changed durable owner state")
				}
			})
		}
	}
}

func TestContributorChangeAndInvalidationRetireCachedProjection(t *testing.T) {
	db := publishingDB(t)
	publishingClient(t, db, true, "secret")
	projection := &publishingProjection{}
	p := Publisher{Settings: &publishingSettings{enabled: true, required: true}, Projection: projection}
	for i := 0; i < 2; i++ {
		if _, _, err := p.GetJSON("secret"); err != nil {
			t.Fatal(err)
		}
	}
	if projection.renders != 1 {
		t.Fatalf("warm render count=%d", projection.renders)
	}
	projection.version++
	if _, _, err := p.GetJSON("secret"); err != nil {
		t.Fatal(err)
	}
	if projection.renders != 2 {
		t.Fatal("contributor change reused stale projection")
	}
	ClearSubscriptionOutputCache()
	if _, _, err := p.GetJSON("secret"); err != nil {
		t.Fatal(err)
	}
	if projection.renders != 3 {
		t.Fatal("explicit invalidation reused stale projection")
	}
}

func TestHTTPDisabledFormatsAndUnknownActionsRejectBeforePublisher(t *testing.T) {
	gin.SetMode(gin.TestMode)
	projection := &publishingProjection{}
	settings := &publishingSettings{}
	p := Publisher{Settings: settings, Projection: projection}
	handler := Handler{Settings: settings, Publisher: p}
	router := gin.New()
	rateCalls := 0
	handler.InitRouter(router.Group(""), func(c *gin.Context) { rateCalls++; c.Next() })
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, path := range []string{"/secret", "/json/secret", "/clash/secret", "/xray/secret", "/secret?format=v2ray", "/secret?format=unknown"} {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			want := http.StatusNotFound
			if strings.Contains(path, "unknown") {
				want = http.StatusBadRequest
			}
			if rec.Code != want {
				t.Fatalf("%s %s: %d", method, path, rec.Code)
			}
		}
	}
	if rateCalls != 12 || projection.calls != 0 {
		t.Fatalf("rate calls=%d, projection effects=%d", rateCalls, projection.calls)
	}
}
