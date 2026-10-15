package web

import (
	"bytes"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/service"
)

type transferDeadlineWriter struct {
	http.ResponseWriter
	writes chan time.Time
}

func (w transferDeadlineWriter) SetReadDeadline(deadline time.Time) error {
	return http.NewResponseController(w.ResponseWriter).SetReadDeadline(deadline)
}
func (w transferDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.writes <- deadline
	return http.NewResponseController(w.ResponseWriter).SetWriteDeadline(deadline)
}

// The real router must retain deadline control through authentication and gzip.
// Separate API handler tests alone cannot prove this production composition.
func TestDatabaseTransferProductionMiddlewarePreservesTransportDeadlines(t *testing.T) {
	for _, protocol := range []string{"http1", "http2"} {
		t.Run(protocol, func(t *testing.T) {
			db := initWebRouterTestDB(t)
			setWebRouterSetting(t, db, "webPath", "/qualification+v1/")
			if err := db.Table("users").Where("id = ?", 1).Update("force_password_reset", false).Error; err != nil {
				t.Fatal(err)
			}
			password := rand.Text()
			if err := (&service.UserService{}).UpdateFirstUser("admin", password); err != nil {
				t.Fatal(err)
			}
			panel, err := NewServer()
			if err != nil {
				t.Fatal(err)
			}
			router, err := panel.initRouter()
			if err != nil {
				t.Fatal(err)
			}
			auth := httptest.NewServer(router)
			t.Cleanup(auth.Close)
			loginRuntimeTransportFixture(t, auth.Client(), auth.URL, password)
			writes := make(chan time.Time, 4)
			s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				router.ServeHTTP(transferDeadlineWriter{ResponseWriter: w, writes: writes}, request)
			}))
			s.Config.WriteTimeout = time.Second
			if protocol == "http2" {
				s.EnableHTTP2 = true
				s.StartTLS()
			} else {
				s.Start()
			}
			t.Cleanup(s.Close)
			s.Client().Jar = auth.Client().Jar
			// The client bounds observation; the one-second server timeout and
			// native multi-minute deadline below are the behavior under test.
			// Full SQLite export can exceed ten seconds on a slow Windows host.
			s.Client().Timeout = 2 * time.Minute
			request, _ := http.NewRequestWithContext(t.Context(), "GET", s.URL+"/qualification+v1/api/getdb", nil)
			request.Header.Set("Accept-Encoding", "gzip")
			response, err := s.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 200 || response.Header.Get("Content-Encoding") != "" || !bytes.HasPrefix(body, []byte("SQLite format 3\x00")) || (protocol == "http2" && response.ProtoMajor != 2) {
				t.Fatal("production database transfer rejected or compressed")
			}
			select {
			case deadline := <-writes:
				if time.Until(deadline) <= time.Minute || time.Until(deadline) > 6*time.Minute {
					t.Fatal("transfer write deadline missing/unbounded")
				}
			default:
				t.Fatal("production compression hid the native deadline controller")
			}
		})
	}
}

func TestDatabaseTransferCompressionExclusionIsExact(t *testing.T) {
	pattern := regexp.MustCompile(databaseTransferUncompressedPattern("/qualification+v1/"))
	for _, namespace := range []string{"api", "apiv2"} {
		for _, action := range []string{"importdb", "getdb", "v1/operations/data/restore", "v1/operations/data/restore/rehearsal"} {
			if !pattern.MatchString("/qualification+v1/" + namespace + "/" + action) {
				t.Fatal("transfer compressed", namespace, action)
			}
		}
	}
	for _, path := range []string{"/qualificationv1/api/importdb", "/qualification+v1/api/getdbExtra", "/qualification+v1/api/importdb/extra", "/qualification+v1/api/clients", "/qualification+v1/api/v1/operations/data/restore/extra"} {
		if pattern.MatchString(path) {
			t.Fatal("unrelated route lost compression", path)
		}
	}
}
