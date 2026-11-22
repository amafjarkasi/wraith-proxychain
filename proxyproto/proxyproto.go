// Package proxyproto implements the HAProxy PROXY protocol v2 (binary) header.
//
// It can wrap a listener to read the client header before HTTP parsing, so a
// CONNECT proxy can see the real client address behind an L4 load balancer.
package proxyproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

var signature = []byte{
	0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A,
	0x51, 0x0D, 0x0A, 0x20, 0x0B,
}

const (
	cmdProxy   = 0x21
	cmdUnknown = 0x20

	afUnspec = 0x0
	afINET   = 0x1
	afINET6  = 0x2

	protoStream = 0x1
)

// WriteHeader writes a PROXY v2 header describing src and dst. If either is
// not a *net.TCPAddr, an UNKNOWN header is written.
func WriteHeader(w io.Writer, src, dst net.Addr) error {
	sa, sok := src.(*net.TCPAddr)
	da, dok := dst.(*net.TCPAddr)
	if !sok || !dok {
		hdr := append([]byte{}, signature...)
		hdr = append(hdr, cmdUnknown, afUnspec<<4, 0x00, 0x00)
		_, err := w.Write(hdr)
		return err
	}

	var fam byte
	var body []byte
	if ip4 := sa.IP.To4(); ip4 != nil && da.IP.To4() != nil {
		fam = afINET
		body = make([]byte, 12)
		copy(body[0:4], ip4)
		copy(body[4:8], da.IP.To4())
		binary.BigEndian.PutUint16(body[8:10], uint16(sa.Port))
		binary.BigEndian.PutUint16(body[10:12], uint16(da.Port))
	} else {
		fam = afINET6
		body = make([]byte, 36)
		copy(body[0:16], sa.IP.To16())
		copy(body[16:32], da.IP.To16())
		binary.BigEndian.PutUint16(body[32:34], uint16(sa.Port))
		binary.BigEndian.PutUint16(body[34:36], uint16(da.Port))
	}

	hdr := make([]byte, 0, 16+len(body))
	hdr = append(hdr, signature...)
	hdr = append(hdr, cmdProxy, fam<<4|protoStream)
	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(body)))
	hdr = append(hdr, length[:]...)
	hdr = append(hdr, body...)
	_, err := w.Write(hdr)
	return err
}

// ReadHeader reads and validates a PROXY v2 header from r. It returns the
// source and destination addresses. For an UNKNOWN header both are nil.
func ReadHeader(r io.Reader) (src, dst net.Addr, err error) {
	sig := make([]byte, len(signature))
	if _, err := io.ReadFull(r, sig); err != nil {
		return nil, nil, fmt.Errorf("proxyproto: read signature: %w", err)
	}
	for i := range signature {
		if sig[i] != signature[i] {
			return nil, nil, errors.New("proxyproto: bad signature")
		}
	}

	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, nil, fmt.Errorf("proxyproto: read header: %w", err)
	}
	length := int(binary.BigEndian.Uint16(head[2:4]))
	if head[0] != cmdProxy {
		if err := discard(r, length); err != nil {
			return nil, nil, err
		}
		return nil, nil, nil
	}

	family := head[1] >> 4
	proto := head[1] & 0x0f
	if proto != protoStream {
		return nil, nil, fmt.Errorf("proxyproto: unsupported protocol 0x%x", proto)
	}

	var need int
	switch family {
	case afINET:
		need = 12
	case afINET6:
		need = 36
	default:
		return nil, nil, fmt.Errorf("proxyproto: unsupported family 0x%x", family)
	}
	if length < need {
		return nil, nil, errors.New("proxyproto: short header")
	}
	body := make([]byte, need)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, nil, fmt.Errorf("proxyproto: read addresses: %w", err)
	}
	if err := discard(r, length-need); err != nil {
		return nil, nil, err
	}
	if family == afINET {
		src = &net.TCPAddr{IP: net.IP(append([]byte{}, body[0:4]...)), Port: int(binary.BigEndian.Uint16(body[8:10]))}
		dst = &net.TCPAddr{IP: net.IP(append([]byte{}, body[4:8]...)), Port: int(binary.BigEndian.Uint16(body[10:12]))}
	} else {
		src = &net.TCPAddr{IP: net.IP(append([]byte{}, body[0:16]...)), Port: int(binary.BigEndian.Uint16(body[32:34]))}
		dst = &net.TCPAddr{IP: net.IP(append([]byte{}, body[16:32]...)), Port: int(binary.BigEndian.Uint16(body[34:36]))}
	}
	return src, dst, nil
}

func discard(r io.Reader, n int) error {
	if n <= 0 {
		return nil
	}
	_, err := io.CopyN(io.Discard, r, int64(n))
	return err
}

// Listener wraps a net.Listener, reading a PROXY v2 header on each accepted
// connection and overriding its RemoteAddr/LocalAddr with the header values.
type Listener struct {
	net.Listener
	timeout time.Duration
}

// NewListener wraps l. timeout bounds the wait for a PROXY header; zero means
// no timeout. A non-positive timeout other than zero is ignored.
func NewListener(l net.Listener, timeout time.Duration) *Listener {
	return &Listener{Listener: l, timeout: timeout}
}

func (l *Listener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.timeout > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(l.timeout))
		}
		src, dst, err := ReadHeader(conn)
		if l.timeout > 0 {
			_ = conn.SetReadDeadline(time.Time{})
		}
		if err != nil {
			conn.Close()
			continue
		}
		return &addrConn{Conn: conn, remote: src, local: dst}, nil
	}
}

type addrConn struct {
	net.Conn
	remote net.Addr
	local  net.Addr
}

func (c *addrConn) RemoteAddr() net.Addr {
	if c.remote != nil {
		return c.remote
	}
	return c.Conn.RemoteAddr()
}

func (c *addrConn) LocalAddr() net.Addr {
	if c.local != nil {
		return c.local
	}
	return c.Conn.LocalAddr()
}
