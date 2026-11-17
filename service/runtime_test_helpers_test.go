package service

import (
	tokenuse "github.com/MalenkiySolovey/solovey-backend/security/tokenuse"
	"testing"
)

func replaceDefaultRuntimeForTest(t testing.TB, runtime *Runtime) {
	t.Helper()
	previous := DefaultRuntime()
	SetDefaultRuntime(runtime)
	t.Cleanup(func() { SetDefaultRuntime(previous) })
}

func resetTokenUseDebouncerForTest() {
	DefaultRuntime().resetTokenUseDebouncer()
	resumeTokenUseFlush()
}

func resumeTokenUseFlush() { tokenuse.Resume() }
