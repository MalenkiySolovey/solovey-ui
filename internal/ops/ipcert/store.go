package ipcert

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func ManagedCertDir() string {
	return filepath.Join(configstorage.GetDBFolderPath(), "certs")
}

func SanitizeIPForFilename(ip string) string {
	replacer := strings.NewReplacer(":", "_", "/", "_", "%", "_")
	return replacer.Replace(strings.TrimSpace(ip))
}

func WriteCertFiles(ip string, certPEM, keyPEM []byte) (certPath, keyPath string, err error) {
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		return "", "", common.NewError("ip cert: empty certificate or key material")
	}
	dir := ManagedCertDir()
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	base := "ip-" + SanitizeIPForFilename(ip)
	certPath = filepath.Join(dir, base+".crt")
	keyPath = filepath.Join(dir, base+".key")
	if err = os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return "", "", err
	}
	if err = os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return "", "", err
	}
	return certPath, keyPath, nil
}

func ParseCertNotAfter(certPEM []byte) (time.Time, error) {
	rest := certPEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return time.Time{}, common.NewError("ip cert: no CERTIFICATE block in PEM")
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return time.Time{}, err
		}
		return cert.NotAfter, nil
	}
}
