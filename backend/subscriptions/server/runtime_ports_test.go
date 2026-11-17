package server

import (
	"github.com/gin-gonic/gin"
	"net"
	"strings"
	"testing"
)

type portTestSettings struct {
	gzipTestSettings
	calls *int
}

func (s portTestSettings) GetSubPath() (string, error) { *s.calls++; return "/sub/", nil }
func (s portTestSettings) GetSubCertFile() (string, error) {
	return "missing-subscription-certificate.pem", nil
}

func TestMissingListenerOwnerRejectsBeforeSettingsOrNetworkEffects(t *testing.T) {
	calls := 0
	runtime := NewRuntimeServer(portTestSettings{calls: &calls}, nil, nil)
	if err := runtime.Start(); err == nil || !strings.Contains(err.Error(), "listener owner is unavailable") {
		t.Fatalf("error = %v", err)
	}
	if calls != 0 || runtime.ListenerReady() {
		t.Fatal("rejected listener start had effects")
	}
}

func TestInvalidTLSClosesTheProductSuppliedListener(t *testing.T) {
	calls := 0
	var listener net.Listener
	runtime := NewRuntimeServer(portTestSettings{calls: &calls}, nil, func() FormatHandlers {
		handler := func(c *gin.Context) { c.Status(200) }
		return FormatHandlers{JSON: handler, Clash: handler, Xray: handler, Headers: handler}
	}, RuntimePorts{
		Listen: func(address, host, port string) (ListenerResult, error) {
			var err error
			listener, err = net.Listen("tcp", "127.0.0.1:0")
			return ListenerResult{Listener: listener, RequestedAddr: address}, err
		},
		WrapAutoHTTPS: func(l net.Listener, _ string) net.Listener { return l },
	})
	if err := runtime.Start(); err == nil {
		t.Fatal("invalid TLS certificate was accepted")
	}
	if listener == nil {
		t.Fatal("fixture did not reach the supplied listener")
	}
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err == nil {
		_ = connection.Close()
		t.Fatal("failed startup leaked a listening socket")
	}
	if runtime.ListenerReady() {
		t.Fatal("failed startup reported listener ready")
	}
}
