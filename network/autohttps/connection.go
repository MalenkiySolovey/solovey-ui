package autohttps

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	firstReadTimeout     = 20 * time.Second
	redirectWriteTimeout = 5 * time.Second
	maxInspectionBytes   = 16 * 1024
)

type AutoHttpsConn struct {
	net.Conn
	firstBuf                    []byte
	firstErr                    error
	bufStart                    int
	authority                   string
	readRequestOnce             sync.Once
	readMu                      sync.Mutex
	deadlineMu                  sync.Mutex
	readDeadline, writeDeadline time.Time
	readBound, writeBound       time.Time
}

func NewAutoHttpsConn(conn net.Conn, authority ...string) net.Conn {
	redirectAuthority := ""
	if len(authority) > 0 {
		redirectAuthority = strings.TrimSpace(authority[0])
	}
	return &AutoHttpsConn{Conn: conn, authority: redirectAuthority}
}

// Only the classification phase is bounded here. Once a non-HTTP prefix is
// replayed, the TLS/stream owner retains its own deadlines and byte semantics.
func (c *AutoHttpsConn) readRequest() bool {
	defer c.endReadInspection()
	if err := c.beginReadInspection(); err != nil {
		c.firstErr = err
		return false
	}
	var chunk [2048]byte
	emptyReads := 0
	for len(c.firstBuf) < maxInspectionBytes {
		n, err := c.Conn.Read(chunk[:min(len(chunk), maxInspectionBytes-len(c.firstBuf))])
		if n > 0 {
			c.firstBuf = append(c.firstBuf, chunk[:n]...)
			emptyReads = 0
		} else {
			emptyReads++
		}
		if err != nil {
			c.firstErr = err
		}
		if !possibleHTTPPrefix(c.firstBuf) {
			return false
		}
		if bytes.Contains(c.firstBuf, []byte("\r\n\r\n")) || bytes.Contains(c.firstBuf, []byte("\n\n")) {
			request, parseErr := http.ReadRequest(bufio.NewReader(bytes.NewReader(c.firstBuf)))
			if parseErr != nil {
				return false
			}
			defer request.Body.Close()
			authority := c.authority
			if authority == "" {
				authority = request.Host
			}
			if !validRedirectAuthority(authority) {
				return false
			}
			target := &url.URL{Scheme: "https", Host: authority, Path: request.URL.Path, RawPath: request.URL.RawPath, RawQuery: request.URL.RawQuery}
			response := http.Response{StatusCode: http.StatusTemporaryRedirect, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, Close: true, Header: http.Header{"Location": {target.String()}}}
			writeErr := c.beginRedirectWrite()
			if writeErr == nil {
				writeErr = response.Write(c.Conn)
			}
			c.endRedirectWrite()
			c.firstErr = writeErr
			if c.firstErr == nil {
				c.firstErr = io.EOF
			}
			_ = c.Conn.Close()
			c.firstBuf = nil
			return true
		}
		if err != nil {
			return false
		}
		if emptyReads >= 3 {
			c.firstErr = io.ErrNoProgress
			return false
		}
	}
	return false
}

func possibleHTTPPrefix(prefix []byte) bool {
	for index, value := range prefix {
		if value == ' ' {
			return index > 0
		}
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(value)) {
			continue
		}
		return false
	}
	return true
}

func validRedirectAuthority(authority string) bool {
	if authority == "" || strings.ContainsAny(authority, "\r\n\t /\\") {
		return false
	}
	parsed, err := url.Parse("https://" + authority)
	return err == nil && parsed.Scheme == "https" && parsed.Host == authority && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func (c *AutoHttpsConn) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	c.readRequestOnce.Do(func() { c.readRequest() })
	if c.firstBuf != nil {
		n := copy(buffer, c.firstBuf[c.bufStart:])
		c.bufStart += n
		if c.bufStart >= len(c.firstBuf) {
			c.firstBuf = nil
		}
		return n, nil
	}
	if c.firstErr != nil {
		err := c.firstErr
		c.firstErr = nil
		return 0, err
	}
	return c.Conn.Read(buffer)
}
