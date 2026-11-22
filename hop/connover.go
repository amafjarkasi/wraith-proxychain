package hop

import (
	"fmt"
	"io"
	"net"
	"strconv"
)

// Address returns host:port for the proxy with a scheme default port.
func (p *Proxy) Address() string {
	if p == nil || p.URL == nil {
		return ""
	}
	host := p.URL.Host
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	port := "80"
	switch p.URL.Scheme {
	case "https", "h2", "https2", "http2":
		port = "443"
	case "socks5", "socks5h":
		port = "1080"
	}
	return net.JoinHostPort(host, port)
}

// ConnectOver tunnels an existing connection to target through proxy p,
// returning the same (tunneled) connection.
func ConnectOver(conn net.Conn, target string, p *Proxy) (net.Conn, error) {
	if conn == nil {
		return nil, fmt.Errorf("hop: nil conn")
	}
	if p == nil || p.URL == nil {
		return nil, fmt.Errorf("hop: nil proxy")
	}
	if target == "" {
		return nil, fmt.Errorf("hop: empty target")
	}
	switch p.URL.Scheme {
	case "http", "https":
		return connectOverHTTP(conn, target, p)
	case "socks5", "socks5h":
		return connectOverSocks5(conn, target, p)
	case "h2", "h2c", "https2", "http2", "http2c":
		return connectOverHTTP2(conn, target, p)
	default:
		return nil, fmt.Errorf("hop: unsupported scheme %q", p.URL.Scheme)
	}
}

func connectOverHTTP(conn net.Conn, target string, p *Proxy) (net.Conn, error) {
	return httpConnect(conn, target, p)
}

func connectOverSocks5(conn net.Conn, target string, p *Proxy) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}

	methods := []byte{0x00}
	u := p.URL.User
	hasAuth := u != nil && u.Username() != ""
	if hasAuth {
		methods = append(methods, 0x02)
	}
	greet := []byte{0x05, byte(len(methods))}
	greet = append(greet, methods...)
	if _, err := conn.Write(greet); err != nil {
		return nil, err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, err
	}
	if resp[0] != 0x05 {
		return nil, fmt.Errorf("hop: bad socks version")
	}
	switch resp[1] {
	case 0x00:
	case 0x02:
		if !hasAuth {
			return nil, fmt.Errorf("hop: proxy requires auth")
		}
		pass, _ := u.Password()
		ub := []byte(u.Username())
		pb := []byte(pass)
		if len(ub) > 255 || len(pb) > 255 {
			return nil, fmt.Errorf("hop: auth too long")
		}
		buf := []byte{0x01, byte(len(ub))}
		buf = append(buf, ub...)
		buf = append(buf, byte(len(pb)))
		buf = append(buf, pb...)
		if _, err := conn.Write(buf); err != nil {
			return nil, err
		}
		ar := make([]byte, 2)
		if _, err := io.ReadFull(conn, ar); err != nil {
			return nil, err
		}
		if ar[1] != 0x00 {
			return nil, fmt.Errorf("hop: socks auth failed")
		}
	default:
		return nil, fmt.Errorf("hop: unsupported auth method 0x%02x", resp[1])
	}

	req := []byte{0x05, 0x01, 0x00}
	ip := net.ParseIP(host)
	if ip == nil {
		if len(host) > 255 {
			return nil, fmt.Errorf("hop: host too long")
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, []byte(host)...)
	} else if v4 := ip.To4(); v4 != nil {
		req = append(req, 0x01)
		req = append(req, v4...)
	} else {
		req = append(req, 0x04)
		req = append(req, ip.To16()...)
	}
	portBytes := make([]byte, 2)
	portBytes[0] = byte(port >> 8)
	portBytes[1] = byte(port)
	req = append(req, portBytes...)
	if _, err := conn.Write(req); err != nil {
		return nil, err
	}
	hr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hr); err != nil {
		return nil, err
	}
	if hr[0] != 0x05 {
		return nil, fmt.Errorf("hop: bad socks version")
	}
	if hr[1] != 0x00 {
		return nil, fmt.Errorf("hop: connect failed 0x%02x", hr[1])
	}
	switch hr[3] {
	case 0x01:
		buf := make([]byte, 4+2)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, err
		}
	case 0x03:
		lb := make([]byte, 1)
		if _, err := io.ReadFull(conn, lb); err != nil {
			return nil, err
		}
		buf := make([]byte, int(lb[0])+2)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, err
		}
	case 0x04:
		buf := make([]byte, 16+2)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("hop: bad atyp")
	}
	return conn, nil
}
