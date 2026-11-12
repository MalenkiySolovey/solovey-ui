//go:build !minimal

package service

import (
	"crypto/tls"
	"crypto/x509"
	"strings"
)

// TLS facts come from the certificate actually installed on the exact owned
// listener; changed settings alone cannot authenticate an effective endpoint.
func (r *Runtime) TLSAcceptsServerName(listen string, port int, serverName string) bool {
	return !r.draining.Load() && r.serving.TLSAcceptsServerName(listen, port, serverName)
}

func (h *backendHTTP) TLSAcceptsServerName(listen string, port int, serverName string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.listeners[runtimeTargetKey(listen, port)]
	return state != nil && state.ready.Load() && state.config.tls && tlsCertificateAcceptsServerName(state.config.certificate, serverName)
}

func tlsCertificateAcceptsServerName(pair *tls.Certificate, serverName string) bool {
	if pair == nil || len(pair.Certificate) == 0 || strings.TrimSpace(serverName) == "" {
		return false
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	return err == nil && leaf.VerifyHostname(serverName) == nil
}

func tlsKeyPairAcceptsServerName(certFile, keyFile, serverName string) bool {
	if strings.TrimSpace(certFile) == "" || strings.TrimSpace(keyFile) == "" || strings.TrimSpace(serverName) == "" {
		return false
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil || len(pair.Certificate) == 0 {
		return false
	}
	return tlsCertificateAcceptsServerName(&pair, serverName)
}
