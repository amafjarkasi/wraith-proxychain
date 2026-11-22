package hop

import (
	"context"
	"encoding/binary"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestSocks5UDPSmoke(t *testing.T) {
	// echo UDP server
	echoConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer echoConn.Close()
	go func() {
		buf := make([]byte, 1024)
		for {
			n, addr, err := echoConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			echoConn.WriteToUDP(buf[:n], addr)
		}
	}()
	echoAddr := echoConn.LocalAddr().(*net.UDPAddr)

	// relay UDP socket
	relayConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer relayConn.Close()
	relayAddr := relayConn.LocalAddr().(*net.UDPAddr)
	go func() {
		buf := make([]byte, 65535)
		for {
			n, clientAddr, err := relayConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			// parse header
			if n < 10 || buf[2] != 0 {
				continue
			}
			atyp := buf[3]
			off := 4
			var host string
			switch atyp {
			case 1:
				host = net.IP(buf[4:8]).String()
				off = 8
			case 3:
				l := int(buf[4])
				host = string(buf[5 : 5+l])
				off = 5 + l
			default:
				continue
			}
			port := int(binary.BigEndian.Uint16(buf[off:]))
			off += 2
			payload := append([]byte{}, buf[off:n]...)
			dstAddr := net.JoinHostPort(host, strconv.Itoa(port))
			// forward
			dc, err := net.Dial("udp", dstAddr)
			if err != nil {
				continue
			}
			dc.Write(payload)
			dc.SetReadDeadline(time.Now().Add(2 * time.Second))
			rbuf := make([]byte, 65535)
			rn, _ := dc.Read(rbuf)
			dc.Close()
			if rn <= 0 {
				continue
			}
			// reply header with source = dst
			ip := net.ParseIP(host)
			var hdr []byte
			if v4 := ip.To4(); v4 != nil {
				hdr = []byte{0, 0, 0, 1}
				hdr = append(hdr, v4...)
			} else {
				hdr = []byte{0, 0, 0, 4}
				hdr = append(hdr, ip.To16()...)
			}
			pb := make([]byte, 2)
			binary.BigEndian.PutUint16(pb, uint16(port))
			hdr = append(hdr, pb...)
			hdr = append(hdr, rbuf[:rn]...)
			relayConn.WriteToUDP(hdr, clientAddr)
		}
	}()

	// SOCKS5 TCP proxy
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				// greeting
				g := make([]byte, 2)
				if _, err := conn.Read(g); err != nil {
					return
				}
				methods := make([]byte, int(g[1]))
				conn.Read(methods)
				conn.Write([]byte{0x05, 0x00})
				// request (assume IPv4 associate, 10 bytes)
				req := make([]byte, 10)
				if _, err := conn.Read(req); err != nil {
					return
				}
				// respond with relay addr
				resp := []byte{0x05, 0x00, 0x00, 0x01}
				resp = append(resp, relayAddr.IP.To4()...)
				pb := make([]byte, 2)
				binary.BigEndian.PutUint16(pb, uint16(relayAddr.Port))
				resp = append(resp, pb...)
				conn.Write(resp)
				// keep control open
				buf := make([]byte, 1024)
				for {
					if _, err := conn.Read(buf); err != nil {
						return
					}
				}
			}(c)
		}
	}()

	p, err := New("socks5://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	pc, err := DialSocks5UDP(context.Background(), p)
	if err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer pc.Close()

	// send to echo server
	msg := []byte("hello-udp")
	if _, err := pc.WriteTo(msg, echoAddr); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1024)
	pc.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, addr, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf[:n]) != "hello-udp" {
		t.Fatalf("echo mismatch: %q", string(buf[:n]))
	}
	t.Logf("reply from %v: %s", addr, string(buf[:n]))
}
