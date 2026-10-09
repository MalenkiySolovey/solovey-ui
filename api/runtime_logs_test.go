package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func TestRuntimeLogHTTPProjectionCancelsAndReleasesSubscriptions(t *testing.T) {
	initSessionTestDB(t)
	completeTokenOwnerResetForTest(t)
	core := coreruntime.NewCore()
	if err := core.Start([]byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Stop() })
	runtime := service.NewRuntime(core)
	users := service.UserService{Runtime: runtime}
	token, err := users.AddToken("admin", 0, "log fixture", "observability")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	NewAPIv2Handler(router.Group("/apiv2"), WithRuntime(runtime))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	generation := core.RuntimeStatus(t.Context()).Generation
	// Exercise Gin's ResponseController unwrap and a stalled transport. The
	// production deadline, rather than browser cooperation, releases the owner.
	stalled := &deadlineRuntimeLogWriter{header: make(http.Header), entered: make(chan time.Time, 1)}
	request := httptest.NewRequest(http.MethodGet, "/apiv2/runtime/logs?generation="+generation, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	finished := make(chan struct{})
	go func() { router.ServeHTTP(stalled, request); close(finished) }()
	select {
	case deadline := <-stalled.entered:
		if remaining := time.Until(deadline); remaining <= 0 || remaining > 2*time.Second {
			t.Fatalf("unbounded transport deadline: %v", remaining)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("log stream did not reach transport")
	}
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("stalled write retained subscription after deadline")
	}
	for range 6 {
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/apiv2/runtime/logs?generation="+generation, nil)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := server.Client().Do(request)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		var event coreruntime.RuntimeLogEvent
		line, readErr := bufio.NewReader(response.Body).ReadBytes('\n')
		cancel()
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || readErr != nil || json.Unmarshal(line, &event) != nil || event.Generation != generation || !event.Reset {
			t.Fatal("log projection failed or orphaned subscriptions exhausted cap", readErr)
		}
	}
}

func TestRuntimeLogHTTP2IdleBetweenFramesDoesNotExpireWriteDeadline(t *testing.T) {
	initSessionTestDB(t)
	completeTokenOwnerResetForTest(t)
	core := coreruntime.NewCore()
	if err := core.Start([]byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Stop() })
	runtime := service.NewRuntime(core)
	users := service.UserService{Runtime: runtime}
	token, err := users.AddToken("admin", 0, "idle log fixture", "observability")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	NewAPIv2Handler(router.Group("/apiv2"), WithRuntime(runtime))
	server := httptest.NewUnstartedServer(router)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	generation := core.RuntimeStatus(t.Context()).Generation
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/apiv2/runtime/logs?generation="+generation, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	reader := bufio.NewReader(response.Body)
	if _, err := reader.ReadBytes('\n'); err != nil || response.ProtoMajor != 2 {
		t.Fatal("HTTP2 stream did not establish", err)
	}
	// The real transport clock establishes an idle interval beyond the per-write
	// bound. A subsequent owner event and frame read prove the stream survives;
	// no sleep-only success assertion or external traffic is used.
	timer := time.NewTimer(2200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	field := reflect.ValueOf(core).Elem().FieldByName("instance")
	box := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface().(*corebox.Box)
	box.LogFactory().NewLogger("idle-fixture").Info("fixture-after-idle")
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal("idle interval incorrectly terminated HTTP2 stream", err)
		}
		if strings.Contains(string(line), "fixture-after-idle") {
			return
		}
	}
}

type deadlineRuntimeLogWriter struct {
	header   http.Header
	deadline time.Time
	entered  chan time.Time
}

func (w *deadlineRuntimeLogWriter) Header() http.Header { return w.header }
func (w *deadlineRuntimeLogWriter) WriteHeader(int)     {}
func (w *deadlineRuntimeLogWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}
func (w *deadlineRuntimeLogWriter) Write([]byte) (int, error) {
	w.entered <- w.deadline
	timer := time.NewTimer(time.Until(w.deadline))
	defer timer.Stop()
	<-timer.C
	return 0, io.ErrClosedPipe
}
