package config

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveeligibility"
	"github.com/gin-gonic/gin"
)

func TestCapabilitySaveErrorPreservesTypedReasonThroughWrapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var received any
	handler := NewHandler(Deps{JSONObj: func(c *gin.Context, obj interface{}, err error) {
		if err == nil {
			t.Fatal("lost rejection")
		}
		received = obj
		c.Status(http.StatusOK)
	}})
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	classified := &saveeligibility.Rejection{Capability: entitycapabilities.Resolve("services", "unknown")}
	if !handler.handleCapabilitySaveError(context, fmt.Errorf("save: %w", classified)) {
		t.Fatal("typed error not handled")
	}
	if received.(map[string]any)["reason"] != "UNKNOWN_ENTITY_TYPE" {
		t.Fatalf("reason=%v", received)
	}
	if handler.handleCapabilitySaveError(context, errors.New("other failure")) {
		t.Fatal("unclassified error consumed")
	}
}
