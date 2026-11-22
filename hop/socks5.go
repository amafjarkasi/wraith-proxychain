package hop

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

func dialSocks5(ctx context.Context, p *Proxy, network, addr string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}

	proxyAddr := p.URL.Host
	if _, _, err := net.SplitHostPort(proxyAddr); err != nil {
		proxyAddr = net.JoinHostPort(proxyAddr, "1080")
	}

	conn, err := p.dialTCP(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}

	// greeting
	methods := []byte{0x00} // no auth
	u := p.URL.User
	hasAuth := u != nil && u.Username() != ""
	if hasAuth {
		methods = append(methods, 0x02)
	}
	greet := []byte{0x05, byte(len(methods))}
	greet = append(greet, methods...)
	if _, err := conn.Write(greet); err != nil {
		conn.Close()
		return nil, err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		conn.Close()
		return nil, err
	}
	if resp[0] != 0x05 {
		conn.Close()
		return nil, errors.New("hop: bad socks version")
	}
	switch resp[1] {
	case 0x00:
	case 0x02:
		if !hasAuth {
			conn.Close()
			return nil, errors.New("hop: proxy requires auth")
		}
		pass, _ := u.Password()
		ub := []byte(u.Username())
		pb := []byte(pass)
		if len(ub) > 255 || len(pb) > 255 {
			conn.Close()
			return nil, errors.New("hop: auth too long")
		}
		buf := []byte{0x01, byte(len(ub))}
		buf = append(buf, ub...)
		buf = append(buf, byte(len(pb)))
		buf = append(buf, pb...)
		if _, err := conn.Write(buf); err != nil {
			conn.Close()
			return nil, err
		}
		ar := make([]byte, 2)
		if _, err := io.ReadFull(conn, ar); err != nil {
			conn.Close()
			return nil, err
		}
		if ar[1] != 0x00 {
			conn.Close()
			return nil, errors.New("hop: socks auth failed")
		}
	default:
		conn.Close()
		return nil, fmt.Errorf("hop: unsupported auth method 0x%02x", resp[1])
	}

	// connect request
	req := []byte{0x05, 0x01, 0x00}
	ip := net.ParseIP(host)
	if ip == nil {
		// domain
		if len(host) > 255 {
			conn.Close()
			return nil, errors.New("hop: host too long")
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
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	req = append(req, portBytes...)
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}

	// read reply header (4 bytes)
	hr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hr); err != nil {
		conn.Close()
		return nil, err
	}
	if hr[0] != 0x05 {
		conn.Close()
		return nil, errors.New("hop: bad socks version")
	}
	if hr[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("hop: connect failed 0x%02x", hr[1])
	}
	// discard bound address
	switch hr[3] {
	case 0x01:
		buf := make([]byte, 4+2)
		if _, err := io.ReadFull(conn, buf); err != nil {
			conn.Close()
			return nil, err
		}
	case 0x03:
		lb := make([]byte, 1)
		if _, err := io.ReadFull(conn, lb); err != nil {
			conn.Close()
			return nil, err
		}
		buf := make([]byte, int(lb[0])+2)
		if _, err := io.ReadFull(conn, buf); err != nil {
			conn.Close()
			return nil, err
		}
	case 0x04:
		buf := make([]byte, 16+2)
		if _, err := io.ReadFull(conn, buf); err != nil {
			conn.Close()
			return nil, err
		}
	default:
		conn.Close()
		return nil, errors.New("hop: bad atyp")
	}
	return conn, nil
}
