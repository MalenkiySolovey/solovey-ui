package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the same composition that Playwright starts, including every
// installed component. Package-level consumer tests cannot detect a missing
// dependency in this separate composition root.
func TestFullComponentCompositionStarts(t *testing.T) {
	if os.Getenv("SUI_E2E_COMPOSITION_TEST_CHILD") == "1" {
		main()
		return
	}

	root := t.TempDir()
	addressPath := filepath.Join(root, "address.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, executable, "-test.run=^TestFullComponentCompositionStarts$")
	command.Dir = root
	command.Env = append(os.Environ(),
		"SUI_E2E_COMPOSITION_TEST_CHILD=1",
		"SUI_DB_FOLDER="+filepath.Join(root, "db"),
		"SUI_COMPONENTS_INSTALLED_FILE="+filepath.Join(root, "components", "installed.json"),
		"APPDATA="+filepath.Join(root, "appdata"),
		"LOCALAPPDATA="+filepath.Join(root, "appdata"),
		"SUI_RUNTIME="+filepath.Join(root, "runtime"),
		"SUI_SECRET="+strings.Repeat("1", 64),
		"SUI_COOKIE_KEY="+base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)),
		"SUI_DISABLE_CORE=1", "XUI_DISABLE_REMOTE=1", "SUI_FORCE_COOKIE_SECURE=false",
		"SUI_E2E_WEB_PATH=/composition-test/", "SUI_E2E_BACKEND_LISTEN=127.0.0.1:0",
		"SUI_E2E_BACKEND_ADDRESS_FILE="+addressPath,
	)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		cancel()
		<-done
	}()

	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			// Return the result for the deferred drain.
			done <- err
			t.Fatalf("component composition exited before readiness: %v\n%s", err, output.String())
		case <-ctx.Done():
			t.Fatal("component composition did not become ready before its deadline")
		case <-ticker.C:
			address, err := os.ReadFile(addressPath)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			endpoint := strings.TrimSpace(string(address))
			if !strings.HasPrefix(endpoint, "127.0.0.1:") {
				t.Fatalf("unexpected fixture listener %q", endpoint)
			}
			client := &http.Client{Transport: &http.Transport{}, Timeout: time.Second}
			response, err := client.Get("http://" + endpoint + "/composition-test/login")
			client.CloseIdleConnections()
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("fixture login returned %d", response.StatusCode)
			}
			return
		}
	}
}
