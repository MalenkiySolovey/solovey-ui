package response

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

func TestJSONMsgBoundsAndRedactsClientErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	Writer{}.JSONMsg(context, "failure", errors.New("password=response-canary "+strings.Repeat("界", MaxClientErrorBytes/3+100)))

	var response Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Success {
		t.Fatalf("unexpected successful response: %#v", response)
	}
	if len(response.Msg) > MaxClientErrorBytes {
		t.Fatalf("client error exceeded response ceiling: %d", len(response.Msg))
	}
	if !utf8.ValidString(response.Msg) {
		t.Fatal("client error ceiling produced invalid UTF-8")
	}
	if strings.Contains(response.Msg, "response-canary") || !strings.Contains(response.Msg, "[REDACTED]") {
		t.Fatalf("client response was not redacted: %q", response.Msg)
	}
}
