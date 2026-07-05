package backupcmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("output", "", "write backup to file path, or '-' for stdout")
	exclude := fs.String("exclude", "", "comma-separated tables to exclude: stats,client_ips,audit,changes")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *output == "" {
		fmt.Fprintln(stderr, "backup: -output is required")
		fs.Usage()
		return 2
	}
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		fmt.Fprintln(stderr, "backup:", err)
		return 1
	}
	backupPath, cleanup, err := dbbackup.PrepareExport(*exclude)
	if err != nil {
		fmt.Fprintln(stderr, "backup:", err)
		return 1
	}
	defer cleanup()

	file, err := os.Open(backupPath) // #nosec G304 -- internal temporary backup path from PrepareExport.
	if err != nil {
		fmt.Fprintln(stderr, "backup:", err)
		return 1
	}
	defer file.Close()

	if *output == "-" {
		if _, err := io.Copy(stdout, file); err != nil {
			fmt.Fprintln(stderr, "backup:", err)
			return 1
		}
		return 0
	}
	if err := writeBackupFile(*output, file); err != nil {
		fmt.Fprintln(stderr, "backup:", err)
		return 1
	}
	fmt.Fprintf(stderr, "backup written to %s\n", *output)
	return 0
}

func writeBackupFile(output string, source io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(output), filepath.Base(output)+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := io.Copy(temp, source); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, output); err != nil {
		return err
	}
	cleanup = false
	return nil
}
