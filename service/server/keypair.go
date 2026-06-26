package server

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/sagernet/sing-box/common/tls"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func (s *ServerService) GenKeypair(keyType string, options string) []string {
	if len(keyType) == 0 {
		return []string{"No keypair to generate"}
	}

	switch keyType {
	case "ech":
		return s.generateECHKeyPair(options)
	case "tls":
		return s.generateTLSKeyPair(options)
	case "reality":
		return s.generateRealityKeyPair()
	case "wireguard":
		return s.generateWireGuardKey(options)
	}

	return []string{"Failed to generate keypair"}
}

func (s *ServerService) generateECHKeyPair(serverName string) []string {
	configPEM, keyPEM, err := tls.ECHKeygenDefault(serverName)
	if err != nil {
		return []string{"Failed to generate ECH keypair: ", err.Error()}
	}
	return append(strings.Split(configPEM, "\n"), strings.Split(keyPEM, "\n")...)
}

func (s *ServerService) generateTLSKeyPair(serverName string) []string {
	privateKeyPEM, publicKeyPEM, err := tls.GenerateCertificate(nil, nil, time.Now, serverName, time.Now().AddDate(0, 12, 0))
	if err != nil {
		return []string{"Failed to generate TLS keypair: ", err.Error()}
	}
	return append(strings.Split(string(privateKeyPEM), "\n"), strings.Split(string(publicKeyPEM), "\n")...)
}

func (s *ServerService) generateRealityKeyPair() []string {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return []string{"Failed to generate Reality keypair: ", err.Error()}
	}
	publicKey := privateKey.PublicKey()
	return []string{"PrivateKey: " + base64.RawURLEncoding.EncodeToString(privateKey[:]), "PublicKey: " + base64.RawURLEncoding.EncodeToString(publicKey[:])}
}

func (s *ServerService) generateWireGuardKey(pk string) []string {
	if len(pk) > 0 {
		key, _ := wgtypes.ParseKey(pk)
		return []string{key.PublicKey().String()}
	}
	wgKeys, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return []string{"Failed to generate wireguard keypair: ", err.Error()}
	}
	return []string{"PrivateKey: " + wgKeys.String(), "PublicKey: " + wgKeys.PublicKey().String()}
}
