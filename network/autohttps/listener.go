package autohttps

import "net"

type AutoHttpsListener struct {
	net.Listener
	authority string
}

func NewAutoHttpsListener(listener net.Listener, authority ...string) net.Listener {
	redirectAuthority := ""
	if len(authority) > 0 {
		redirectAuthority = authority[0]
	}
	return &AutoHttpsListener{
		Listener:  listener,
		authority: redirectAuthority,
	}
}

func (l *AutoHttpsListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return NewAutoHttpsConn(conn, l.authority), nil
}
