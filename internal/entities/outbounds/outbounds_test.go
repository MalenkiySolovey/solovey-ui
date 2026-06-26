package outbounds

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

type fakeOutboundCore struct {
	running bool
	calls   []string
	err     error
}

func (c *fakeOutboundCore) IsRunning() bool { return c.running }

func (c *fakeOutboundCore) RemoveOutbound(tag string) error {
	c.calls = append(c.calls, tag)
	return c.err
}

func (c *fakeOutboundCore) AddOutbound([]byte) error { return nil }

func TestRemoveFromCore(t *testing.T) {
	core := &fakeOutboundCore{running: true}
	if err := RemoveFromCore([]string{"a", "b"}, core); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(core.calls, []string{"a", "b"}) {
		t.Fatalf("calls = %#v", core.calls)
	}
}

func TestRemoveFromCoreIgnoresMissingOutbound(t *testing.T) {
	core := &fakeOutboundCore{running: true, err: os.ErrInvalid}
	if err := RemoveFromCore([]string{"gone"}, core); err != nil {
		t.Fatalf("missing outbound should be ignored: %v", err)
	}
}

func TestRemoveFromCoreReturnsRealError(t *testing.T) {
	want := errors.New("boom")
	core := &fakeOutboundCore{running: true, err: want}
	if err := RemoveFromCore([]string{"bad"}, core); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestRemoveFromCoreSkipsStoppedCore(t *testing.T) {
	core := &fakeOutboundCore{}
	if err := RemoveFromCore([]string{"a"}, core); err != nil {
		t.Fatal(err)
	}
	if len(core.calls) != 0 {
		t.Fatalf("stopped core should not be called: %#v", core.calls)
	}
}
