package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type gzipTestSettings struct{}

func (gzipTestSettings) GetSubPath() (string, error)      { return "/sub/", nil }
func (gzipTestSettings) GetSubDomain() (string, error)    { return "", nil }
func (gzipTestSettings) GetSubJsonPath() (string, error)  { return "/custom-json/", nil }
func (gzipTestSettings) GetSubClashPath() (string, error) { return "/custom-clash/", nil }
func (gzipTestSettings) GetSubXrayPath() (string, error)  { return "/custom-xray/", nil }
func (gzipTestSettings) GetSubCertFile() (string, error)  { return "", nil }
func (gzipTestSettings) GetSubKeyFile() (string, error)   { return "", nil }
func (gzipTestSettings) GetSubListen() (string, error)    { return "127.0.0.1", nil }
func (gzipTestSettings) GetSubPort() (int, error)         { return 0, nil }

func TestRuntimeServerCompressesSubscriptionPayloads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := strings.Repeat("vless://compressible\n", 200)
	server := NewRuntimeServer(gzipTestSettings{}, func(group *gin.RouterGroup) {
		group.GET("/:subid", func(c *gin.Context) { c.String(http.StatusOK, payload) })
	}, func() FormatHandlers {
		return FormatHandlers{
			JSON:    func(c *gin.Context) { c.String(http.StatusOK, payload) },
			Clash:   func(c *gin.Context) { c.String(http.StatusOK, payload) },
			Xray:    func(c *gin.Context) { c.String(http.StatusOK, payload) },
			Headers: func(c *gin.Context) { c.Status(http.StatusOK) },
		}
	})
	engine, err := server.InitRouter()
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/sub/client", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	engine.ServeHTTP(recorder, request)
	if recorder.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("response was not gzipped: headers=%v", recorder.Header())
	}
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != payload {
		t.Fatal("gzip response changed subscription payload")
	}
}
