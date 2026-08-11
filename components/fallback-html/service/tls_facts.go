//go:build !minimal

package service

import (
	"crypto/tls"
	"crypto/x509"
	"strings"
)

// RuntimeTLSAcceptsServerName reports only whether the exact certificate/key
// pair used by the fallback runtime authenticates serverName. It deliberately
// returns no certificate or host-local path details.
func RuntimeTLSAcceptsServerName(serverName string) bool {
	certFile, keyFile := runtimeTLSFiles()
	return tlsKeyPairAcceptsServerName(certFile, keyFile, serverName)
}

func tlsKeyPairAcceptsServerName(certFile, keyFile, serverName string) bool {
	if strings.TrimSpace(certFile) == "" || strings.TrimSpace(keyFile) == "" || strings.TrimSpace(serverName) == "" {
		return false
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil || len(pair.Certificate) == 0 {
		return false
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	return err == nil && leaf.VerifyHostname(serverName) == nil
}
