package main

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("SOLOVEY_TEST_CLI_PROCESS") != "1" {
		return
	}
	for index, value := range os.Args {
		if value == "--" {
			os.Args = append([]string{os.Args[0]}, os.Args[index+1:]...)
			main()
			return
		}
	}
	t.Fatal("missing command separator")
}

func runCLIProcess(t *testing.T, folder string, args ...string) (string, error) {
	t.Helper()
	command := exec.Command(os.Args[0], append([]string{"-test.run=^TestCLIHelperProcess$", "--"}, args...)...)
	command.Env = append(os.Environ(), "SOLOVEY_TEST_CLI_PROCESS=1", "SUI_DB_FOLDER="+folder)
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestCLICommandDatabaseLifecycle(t *testing.T) {
	for _, scenario := range []struct {
		name string
		args []string
		want []string
	}{
		{"admin-update", []string{"admin", "-username", "cli-fixture", "-password", "CLI-fixture-password-42"}, []string{"reset admin credentials success", "Username:\t cli-fixture", "Password is hashed"}},
		{"settings-update", []string{"setting", "-port", "39241", "-path", "/cli-fixture/", "-subPort", "39242", "-subPath", "/cli-sub/"}, []string{"set port success", "Current panel settings:", "39241", "/cli-fixture/", "39242", "/cli-sub/"}},
		{"clear-domain", []string{"setting", "-clearDomain"}, []string{"clear panel domain and address success", "Current panel settings:"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			folder := t.TempDir()
			output, err := runCLIProcess(t, folder, scenario.args...)
			if err != nil {
				t.Fatalf("command failed after mutation: %v: %s", err, output)
			}
			for _, want := range scenario.want {
				if !strings.Contains(output, want) {
					t.Fatalf("missing result %q: %s", want, output)
				}
			}
			if strings.Contains(output, "CLI-fixture-password-42") || strings.Contains(output, "$2a$") || strings.Contains(output, "$2b$") {
				t.Fatal("command exposed a password or hash")
			}
			show := []string{scenario.args[0], "-show"}
			if output, err = runCLIProcess(t, folder, show...); err != nil {
				t.Fatalf("independent command could not reopen persisted state: %v: %s", err, output)
			}
		})
	}
}

func TestCLIRejectsFutureSchemaBeforeSettingsMutation(t *testing.T) {
	folder := t.TempDir()
	if output, err := runCLIProcess(t, folder, "setting", "-show"); err != nil {
		t.Fatalf("initialize fixture: %v: %s", err, output)
	}
	database, err := sql.Open("sqlite3", filepath.Join(folder, "solovey-ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec("INSERT INTO settings(key,value) VALUES('webPort','39243') ON CONFLICT(key) DO UPDATE SET value='39243'"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE settings SET value='9999.0' WHERE key='coreSchemaVersion'"); err != nil {
		t.Fatal(err)
	}
	if output, err := runCLIProcess(t, folder, "setting", "-port", "39244"); err == nil || strings.Contains(output, "set port success") {
		t.Fatalf("future state admitted a mutation: %v: %s", err, output)
	}
	var port string
	if err := database.QueryRow("SELECT value FROM settings WHERE key='webPort'").Scan(&port); err != nil || port != "39243" {
		t.Fatalf("future state changed: port=%q err=%v", port, err)
	}
}
