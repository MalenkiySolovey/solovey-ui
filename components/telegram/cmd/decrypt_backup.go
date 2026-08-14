//go:build !minimal

package telegramcmd

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	optionalcmd "github.com/MalenkiySolovey/solovey-ui/componenthost/commands"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"golang.org/x/term"
)

func init() {
	optionalcmd.Register(optionalcmd.Command{
		Name:      "decrypt-backup",
		UsageLine: "    decrypt-backup decrypt encrypted backup envelope",
		Run:       RunDecryptBackup,
	})
}

func RunDecryptBackup(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("decrypt-backup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var inPath string
	var outPath string
	var passphraseStdin bool
	var passphraseEnv string
	fs.StringVar(&inPath, "in", "", "path to encrypted backup envelope")
	fs.StringVar(&outPath, "out", "", "path to decrypted SQLite database")
	fs.BoolVar(&passphraseStdin, "passphrase-stdin", false, "read backup passphrase from stdin")
	fs.StringVar(&passphraseEnv, "passphrase-env", "", "environment variable containing backup passphrase")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "decrypt-backup: unexpected positional arguments")
		return 2
	}
	if strings.TrimSpace(inPath) == "" || strings.TrimSpace(outPath) == "" {
		fmt.Fprintln(stderr, "decrypt-backup: --in and --out are required")
		return 2
	}
	if passphraseStdin && strings.TrimSpace(passphraseEnv) != "" {
		fmt.Fprintln(stderr, "decrypt-backup: use only one of --passphrase-stdin or --passphrase-env")
		return 2
	}

	passphrase, err := readDecryptBackupPassphrase(stdin, stderr, getenv, passphraseStdin, passphraseEnv)
	if err != nil {
		fmt.Fprintln(stderr, "decrypt-backup:", err)
		return 2
	}
	defer common.WipeBytes(passphrase)
	if len(passphrase) == 0 {
		fmt.Fprintln(stderr, "decrypt-backup: empty passphrase")
		return 2
	}

	if _, err := os.Lstat(outPath); err == nil {
		fmt.Fprintln(stderr, "decrypt-backup: output already exists")
		return 1
	} else if !os.IsNotExist(err) {
		fmt.Fprintln(stderr, "decrypt-backup:", err)
		return 1
	}

	// #nosec G304 -- inPath is a CLI argument supplied by the operator.
	input, err := os.Open(inPath)
	if err != nil {
		fmt.Fprintln(stderr, "decrypt-backup:", err)
		return 1
	}
	defer input.Close()
	if err := decryptBackupToNewFile(input, outPath, passphrase); err != nil {
		fmt.Fprintln(stderr, "decrypt-backup:", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "decrypt-backup: wrote", outPath)
	return 0
}

func readDecryptBackupPassphrase(stdin io.Reader, stderr io.Writer, getenv func(string) string, passphraseStdin bool, passphraseEnv string) ([]byte, error) {
	if strings.TrimSpace(passphraseEnv) != "" {
		return []byte(getenv(passphraseEnv)), nil
	}
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fmt.Fprint(stderr, "Backup passphrase: ")
		passphrase, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return nil, err
		}
		return passphrase, nil
	}
	if !passphraseStdin {
		fmt.Fprintln(stderr, "decrypt-backup: reading passphrase from stdin")
	}
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return nil, err
	}
	return bytes.TrimRight(raw, "\r\n"), nil
}

func decryptBackupToNewFile(input *os.File, outPath string, passphrase []byte) error {
	if input == nil {
		return backupenvelope.ErrInvalidEnvelope
	}
	dir := filepath.Dir(outPath)
	base := filepath.Base(outPath)
	temp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := decryptBackupEnvelope(temp, input, passphrase); err != nil {
		_ = temp.Close()
		return backupenvelope.ErrDecryptionFailed
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Link(tempPath, outPath); err != nil {
		if _, statErr := os.Lstat(outPath); statErr == nil {
			return fmt.Errorf("output already exists")
		}
		return err
	}
	return nil
}

func decryptBackupEnvelope(destination io.Writer, input *os.File, passphrase []byte) error {
	prefix := make([]byte, len(backupenvelope.Magic)+1)
	if _, err := io.ReadFull(input, prefix); err != nil || !backupenvelope.IsEnvelope(prefix) {
		return backupenvelope.ErrInvalidEnvelope
	}
	version := prefix[len(backupenvelope.Magic)]
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if version == backupenvelope.VersionStream {
		_, _, err := backupenvelope.OpenStream(destination, input, passphrase, backupenvelope.MaxStreamBytes)
		return err
	}
	if version != backupenvelope.Version {
		return backupenvelope.ErrInvalidEnvelope
	}
	if stat, err := input.Stat(); err != nil {
		return err
	} else if stat.Size() > backupenvelope.LegacyMaxBytes {
		return backupenvelope.ErrInvalidEnvelope
	}
	envelope, err := io.ReadAll(io.LimitReader(input, backupenvelope.LegacyMaxBytes+1))
	if err != nil || int64(len(envelope)) > backupenvelope.LegacyMaxBytes {
		common.WipeBytes(envelope)
		return backupenvelope.ErrInvalidEnvelope
	}
	defer common.WipeBytes(envelope)
	plaintext, err := backupenvelope.Open(envelope, passphrase)
	if err != nil {
		return err
	}
	defer common.WipeBytes(plaintext)
	_, err = io.Copy(destination, bytes.NewReader(plaintext))
	return err
}
