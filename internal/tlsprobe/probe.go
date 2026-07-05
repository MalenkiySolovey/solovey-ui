package tlsprobe

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/ssrf"
)

type Result struct {
	LeafHash string `json:"leafHash"`
}

type ProbeConfig struct {
	Server     string
	Port       string
	ServerName string
	Timeout    time.Duration
}

func Probe(ctx context.Context, config ProbeConfig) (Result, error) {
	target, port, err := normalizeTarget(config.Server, config.Port)
	if err != nil {
		return Result{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	addrs, err := safeTargetAddresses(ctx, target, port)
	if err != nil {
		return Result{}, err
	}
	serverName := strings.TrimSpace(config.ServerName)
	if serverName == "" && net.ParseIP(target) == nil {
		serverName = target
	}
	leaf, err := dialLeafCertificate(ctx, addrs, port, serverName)
	if err != nil {
		return Result{}, err
	}
	return Result{LeafHash: CertificatePublicKeySHA256(leaf)}, nil
}

func CertificatePublicKeySHA256(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:])
}

func normalizeTarget(server string, rawPort string) (string, string, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		return "", "", fmt.Errorf("server is empty")
	}
	if strings.Contains(server, "://") || strings.ContainsAny(server, "/?#") {
		return "", "", fmt.Errorf("server must be a hostname or IP address")
	}
	host := server
	if splitHost, splitPort, err := net.SplitHostPort(server); err == nil {
		host = strings.Trim(splitHost, "[]")
		if rawPort == "" {
			rawPort = splitPort
		}
	}
	rawPort = strings.TrimSpace(rawPort)
	if rawPort == "" {
		rawPort = "443"
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port <= 0 || port > 65535 {
		return "", "", fmt.Errorf("invalid port")
	}
	return host, strconv.Itoa(port), nil
}

func safeTargetAddresses(ctx context.Context, host string, port string) ([]netip.Addr, error) {
	rawURL := (&url.URL{
		Scheme: "https",
		Host:   net.JoinHostPort(host, port),
		Path:   "/",
	}).String()
	if err := ssrf.ValidateOutboundURL(ctx, rawURL, "https"); err != nil {
		return nil, err
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		if ssrf.IsBlockedAddr(addr) {
			return nil, fmt.Errorf("server address is not allowed")
		}
		return []netip.Addr{addr}, nil
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	addrs := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip.IP)
		if !ok {
			return nil, fmt.Errorf("server resolved to an invalid address")
		}
		addr = addr.Unmap()
		if ssrf.IsBlockedAddr(addr) {
			return nil, fmt.Errorf("server resolves to a disallowed IP")
		}
		addrs = append(addrs, addr)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("server did not resolve")
	}
	return addrs, nil
}

func dialLeafCertificate(ctx context.Context, addrs []netip.Addr, port string, serverName string) (*x509.Certificate, error) {
	dialer := net.Dialer{}
	var lastErr error
	for _, addr := range addrs {
		tcpConn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(addr.String(), port))
		if err != nil {
			lastErr = err
			continue
		}
		tlsConn := tls.Client(tcpConn, &tls.Config{
			InsecureSkipVerify: true, // #nosec G402 -- this endpoint captures a pin, it does not authenticate the peer.
			NextProtos:         []string{"h2", "http/1.1"},
			ServerName:         serverName,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = tlsConn.Close()
			lastErr = err
			continue
		}
		state := tlsConn.ConnectionState()
		_ = tlsConn.Close()
		if len(state.PeerCertificates) == 0 {
			lastErr = fmt.Errorf("server returned no certificates")
			continue
		}
		return state.PeerCertificates[0], nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("server did not resolve")
}
