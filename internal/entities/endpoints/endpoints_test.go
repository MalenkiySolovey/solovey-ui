package endpoints

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

type fakeEndpointCore struct {
	running bool
	calls   []string
	err     error
}

func (c *fakeEndpointCore) IsRunning() bool { return c.running }

func (c *fakeEndpointCore) RemoveEndpoint(tag string) error {
	c.calls = append(c.calls, tag)
	return c.err
}

func (c *fakeEndpointCore) AddEndpoint([]byte) error { return nil }

func TestRemoveFromCore(t *testing.T) {
	core := &fakeEndpointCore{running: true}
	if err := RemoveFromCore([]string{"a", "b"}, core); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(core.calls, []string{"a", "b"}) {
		t.Fatalf("calls = %#v", core.calls)
	}
}

func TestRemoveFromCoreIgnoresMissingEndpoint(t *testing.T) {
	core := &fakeEndpointCore{running: true, err: os.ErrInvalid}
	if err := RemoveFromCore([]string{"gone"}, core); err != nil {
		t.Fatalf("missing endpoint should be ignored: %v", err)
	}
}

func TestRemoveFromCoreReturnsRealError(t *testing.T) {
	want := errors.New("boom")
	core := &fakeEndpointCore{running: true, err: want}
	if err := RemoveFromCore([]string{"bad"}, core); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestRemoveFromCoreSkipsStoppedCore(t *testing.T) {
	core := &fakeEndpointCore{}
	if err := RemoveFromCore([]string{"a"}, core); err != nil {
		t.Fatal(err)
	}
	if len(core.calls) != 0 {
		t.Fatalf("stopped core should not be called: %#v", core.calls)
	}
}
