package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/gin-gonic/gin"
)

func TestRuntimeParentRequestEnvelopeIsBoundedAndStrict(t *testing.T) {
	request := coreruntime.DisconnectRequest{Generation: "00000000-0000-4000-8000-000000000001", ClientID: 7}
	for index := range 128 {
		request.Parents = append(request.Parents, coreruntime.QUICParentTarget{ParentID: strconv.Itoa(index + 1), Inbound: strings.Repeat("i", 256), Epoch: request.Generation})
	}
	valid, err := json.Marshal(request)
	if err != nil || len(valid) <= 2048 || len(valid) > 64*1024 {
		t.Fatal("fixture did not prove larger bounded parent envelope")
	}
	decode := func(body string) bool {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodPost, "/runtime/disconnect", strings.NewReader(body))
		var result coreruntime.DisconnectRequest
		return decodeRuntimeDisconnect(context, &result)
	}
	if !decode(string(valid)) {
		t.Fatal("bounded128 parent scopes rejected")
	}
	for _, invalid := range []string{string(valid) + `{}`, strings.Replace(string(valid), `"parentId":"1"`, `"parentId":"1","unknown":true`, 1), strings.Repeat(" ", 64*1024) + string(valid), strings.Repeat(" ", 2048) + `{"generation":"` + request.Generation + `","clientId":7}`} {
		if decode(invalid) {
			t.Fatal("oversized/trailing/unknown or oversized flow-only request accepted")
		}
	}
}
