package runtime

import (
	"testing"

	"github.com/sagernet/sing-box/daemon"
)

func TestRuntimeLogProjectionPreservesPublicLevels(t *testing.T) {
	for name, want := range map[string]int32{"PANIC": 0, "FATAL": 1, "ERROR": 2, "WARN": 3, "NOTICE": 4, "INFO": 4, "DEBUG": 5, "TRACE": 6} {
		if value, present := daemon.LogLevel_value[name]; present {
			if got := runtimeLogLevel(daemon.LogLevel(value)); got != want {
				t.Errorf("%s public level = %d, want %d", name, got, want)
			}
		}
	}
	if runtimeLogLevel(daemon.LogLevel(12345)) != -1 {
		t.Fatal("unknown provider level reported as a known severity")
	}
}
