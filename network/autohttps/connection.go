package autohttps

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type AutoHttpsConn struct {
	net.Conn

	firstBuf  []byte
	bufStart  int
	authority string

	readRequestOnce sync.Once
}

func NewAutoHttpsConn(conn net.Conn, authority ...string) net.Conn {
	redirectAuthority := ""
	if len(authority) > 0 {
		redirectAuthority = strings.TrimSpace(authority[0])
	}
	return &AutoHttpsConn{
		Conn:      conn,
		authority: redirectAuthority,
	}
}

func (c *AutoHttpsConn) readRequest() bool {
	c.firstBuf = make([]byte, 2048)
	n, err := c.Conn.Read(c.firstBuf)
	c.firstBuf = c.firstBuf[:n]
	if err != nil {
		return false
	}
	reader := bytes.NewReader(c.firstBuf)
	bufReader := bufio.NewReader(reader)
	request, err := http.ReadRequest(bufReader)
	if err != nil {
		return false
	}
	authority := c.authority
	if authority == "" {
		authority = request.Host
	}
	if !validRedirectAuthority(authority) {
		return false
	}
	target := &url.URL{Scheme: "https", Host: authority, Path: request.URL.Path, RawPath: request.URL.RawPath, RawQuery: request.URL.RawQuery}
	resp := http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{}}
	location := target.String()
	resp.Header.Set("Location", location)
	_ = resp.Write(c.Conn)
	_ = c.Close()
	c.firstBuf = nil
	return true
}

func validRedirectAuthority(authority string) bool {
	if authority == "" || strings.ContainsAny(authority, "\r\n\t /\\") {
		return false
	}
	parsed, err := url.Parse("https://" + authority)
	return err == nil && parsed.Scheme == "https" && parsed.Host == authority && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func (c *AutoHttpsConn) Read(buf []byte) (int, error) {
	c.readRequestOnce.Do(func() {
		c.readRequest()
	})

	if c.firstBuf != nil {
		n := copy(buf, c.firstBuf[c.bufStart:])
		c.bufStart += n
		if c.bufStart >= len(c.firstBuf) {
			c.firstBuf = nil
		}
		return n, nil
	}

	return c.Conn.Read(buf)
}
