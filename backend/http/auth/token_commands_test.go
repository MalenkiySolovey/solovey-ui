package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type tokenCommandFixture struct {
	UserCommands
	create func(string, int64, string, string) (string, error)
}

func (f tokenCommandFixture) AddToken(user string, expiry int64, desc, scope string) (string, error) {
	return f.create(user, expiry, desc, scope)
}

func TestTokenCommandKeepsAuthorizationBeforeCommitAndPublicationAfterCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name        string
		authorized  bool
		commitError error
	}{
		{name: "denied", authorized: false},
		{name: "failed commit", authorized: true, commitError: errors.New("transaction rejected")},
		{name: "committed", authorized: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var order []string
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			values := url.Values{"expiry": {"3"}, "desc": {"test"}, "scope": {"read"}}
			c.Request = httptest.NewRequest(http.MethodPost, "/api/addToken", strings.NewReader(values.Encode()))
			c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			handler := NewHandler(Deps{
				UserService: tokenCommandFixture{create: func(user string, expiry int64, desc, scope string) (string, error) {
					order = append(order, "commit")
					if user != "authenticated-admin" || expiry != 3 || desc != "test" || scope != "read" {
						t.Fatalf("command input = %q %d %q %q", user, expiry, desc, scope)
					}
					return "one-time-secret", test.commitError
				}},
				LoginUser: func(*gin.Context) string { return "authenticated-admin" },
				RequireStepUp: func(_ *gin.Context, operation, target string) bool {
					order = append(order, "authorize")
					if operation != "token.create" || target != "new-token" {
						t.Fatalf("authorization = %s %s", operation, target)
					}
					return test.authorized
				},
				ReloadTokens: func() { order = append(order, "reload") },
				Audit:        func(_ *gin.Context, _, _, _, _ string, _ map[string]any) { order = append(order, "audit") },
				JSONObj: func(_ *gin.Context, _ interface{}, err error) {
					order = append(order, "response")
					if !errors.Is(err, test.commitError) {
						t.Fatalf("commit error changed: %v", err)
					}
				},
			})
			handler.AddToken(c)
			want := "authorize"
			if test.authorized {
				want = ",commit,response"
				if test.commitError == nil {
					want = ",commit,reload,audit,response"
				}
				want = "authorize" + want
			}
			if got := strings.Join(order, ","); got != want {
				t.Fatalf("effect order=%q want=%q", got, want)
			}
		})
	}
}
