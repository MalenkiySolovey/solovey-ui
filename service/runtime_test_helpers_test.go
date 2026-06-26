package service

import "testing"

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
