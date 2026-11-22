package hop

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// DialSocks5UDP performs a SOCKS5 UDP ASSOCIATE through p and returns a PacketConn.
func DialSocks5UDP(ctx context.Context, p *Proxy) (net.PacketConn, error) {
	if p == nil || p.URL == nil {
		return nil, fmt.Errorf("hop: nil proxy")
	}
	proxyAddr := p.Address()
	conn, err := p.dialTCP(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}

	// greeting
	methods := []byte{0x00}
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

	// UDP associate request: DST 0.0.0.0:0
	req := []byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}
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
		return nil, fmt.Errorf("hop: associate failed 0x%02x", hr[1])
	}
	var host string
	switch hr[3] {
	case 0x01:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			conn.Close()
			return nil, err
		}
		host = net.IP(buf).String()
	case 0x03:
		lb := make([]byte, 1)
		if _, err := io.ReadFull(conn, lb); err != nil {
			conn.Close()
			return nil, err
		}
		buf := make([]byte, int(lb[0]))
		if _, err := io.ReadFull(conn, buf); err != nil {
			conn.Close()
			return nil, err
		}
		host = string(buf)
	case 0x04:
		buf := make([]byte, 16)
		if _, err := io.ReadFull(conn, buf); err != nil {
			conn.Close()
			return nil, err
		}
		host = net.IP(buf).String()
	default:
		conn.Close()
		return nil, errors.New("hop: bad atyp")
	}
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		conn.Close()
		return nil, err
	}
	port := int(binary.BigEndian.Uint16(portBuf))
	relayAddr := net.JoinHostPort(host, strconv.Itoa(port))
	relayUDP, err := net.ResolveUDPAddr("udp", relayAddr)
	if err != nil {
		conn.Close()
		return nil, err
	}

	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &socks5UDPConn{control: conn, udp: udpConn, relay: relayUDP}, nil
}

type socks5UDPConn struct {
	control net.Conn
	udp     *net.UDPConn
	relay   *net.UDPAddr
}

func (c *socks5UDPConn) ReadFrom(p []byte) (int, net.Addr, error) {
	tmp := make([]byte, len(p)+512)
	n, _, err := c.udp.ReadFromUDP(tmp)
	if err != nil {
		return 0, nil, err
	}
	if n < 4 {
		return 0, nil, errors.New("hop: short udp header")
	}
	if tmp[2] != 0 {
		return 0, nil, errors.New("hop: fragmented udp not supported")
	}
	atyp := tmp[3]
	off := 4
	var host string
	switch atyp {
	case 0x01:
		if n < off+4+2 {
			return 0, nil, errors.New("hop: short udp header")
		}
		host = net.IP(tmp[off : off+4]).String()
		off += 4
	case 0x03:
		if n < off+1 {
			return 0, nil, errors.New("hop: short udp header")
		}
		l := int(tmp[off])
		off++
		if n < off+l+2 {
			return 0, nil, errors.New("hop: short udp header")
		}
		host = string(tmp[off : off+l])
		off += l
	case 0x04:
		if n < off+16+2 {
			return 0, nil, errors.New("hop: short udp header")
		}
		host = net.IP(tmp[off : off+16]).String()
		off += 16
	default:
		return 0, nil, errors.New("hop: bad atyp")
	}
	if n < off+2 {
		return 0, nil, errors.New("hop: short udp header")
	}
	port := int(binary.BigEndian.Uint16(tmp[off:]))
	off += 2
	src, _ := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	k := copy(p, tmp[off:n])
	return k, src, nil
}

func (c *socks5UDPConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	host, portStr, err := net.SplitHostPort(addr.String())
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, err
	}
	buf := []byte{0, 0, 0}
	ip := net.ParseIP(host)
	if ip == nil {
		if len(host) > 255 {
			return 0, errors.New("hop: host too long")
		}
		buf = append(buf, 0x03, byte(len(host)))
		buf = append(buf, []byte(host)...)
	} else if v4 := ip.To4(); v4 != nil {
		buf = append(buf, 0x01)
		buf = append(buf, v4...)
	} else {
		buf = append(buf, 0x04)
		buf = append(buf, ip.To16()...)
	}
	pb := make([]byte, 2)
	binary.BigEndian.PutUint16(pb, uint16(port))
	buf = append(buf, pb...)
	buf = append(buf, p...)
	_, err = c.udp.WriteToUDP(buf, c.relay)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *socks5UDPConn) Close() error {
	c.udp.Close()
	return c.control.Close()
}
func (c *socks5UDPConn) LocalAddr() net.Addr { return c.udp.LocalAddr() }
func (c *socks5UDPConn) SetDeadline(t time.Time) error {
	return c.udp.SetDeadline(t)
}
func (c *socks5UDPConn) SetReadDeadline(t time.Time) error {
	return c.udp.SetReadDeadline(t)
}
func (c *socks5UDPConn) SetWriteDeadline(t time.Time) error {
	return c.udp.SetWriteDeadline(t)
}
