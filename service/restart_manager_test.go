package service

import (
	"testing"
)

func TestInProcessRestartCallbackRunsWhenRegistered(t *testing.T) {
	called := false
	SetInProcessRestart(func() { called = true })
	t.Cleanup(func() { SetInProcessRestart(nil) })

	if !runInProcessRestart() {
		t.Fatal("expected registered in-process restart to run")
	}
	if !called {
		t.Fatal("registered in-process restart callback was not called")
	}
}

func TestInProcessRestartCallbackReportsMissingRegistration(t *testing.T) {
	SetInProcessRestart(nil)

	if runInProcessRestart() {
		t.Fatal("did not expect in-process restart without a registered callback")
	}
}
