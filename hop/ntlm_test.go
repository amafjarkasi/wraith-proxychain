package hop

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func ntlmChallenge(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 48)
	copy(b, "NTLMSSP\x00")
	binary.LittleEndian.PutUint32(b[8:], 2)          // CHALLENGE
	binary.LittleEndian.PutUint32(b[20:], 0x0880201) // unicode|ntlm|ext-session-security|target-info
	copy(b[24:], "12345678")                         // server challenge
	return b
}

func TestNTLMConnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	challenge := ntlmChallenge(t)
	stage := 0
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		for {
			req, err := http.ReadRequest(br)
			if err != nil {
				done <- err
				return
			}
			auth := req.Header.Get("Proxy-Authorization")
			switch stage {
			case 0:
				if !strings.HasPrefix(auth, "Basic ") {
					done <- &testErr{"expected Basic first"}
					return
				}
				stage = 1
				writeProxyResponse(conn, http.StatusProxyAuthRequired, "NTLM")
			case 1:
				if !strings.HasPrefix(auth, "NTLM ") {
					done <- &testErr{"expected NTLM negotiate"}
					return
				}
				msg, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "NTLM "))
				if err != nil || len(msg) < 12 || !strings.HasPrefix(string(msg[:8]), "NTLMSSP") || binary.LittleEndian.Uint32(msg[8:]) != 1 {
					done <- &testErr{"bad negotiate message"}
					return
				}
				stage = 2
				writeProxyResponse(conn, http.StatusProxyAuthRequired, "NTLM "+base64.StdEncoding.EncodeToString(challenge))
			case 2:
				if !strings.HasPrefix(auth, "NTLM ") {
					done <- &testErr{"expected NTLM authenticate"}
					return
				}
				msg, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "NTLM "))
				if err != nil || len(msg) < 12 || !strings.HasPrefix(string(msg[:8]), "NTLMSSP") || binary.LittleEndian.Uint32(msg[8:]) != 3 {
					done <- &testErr{"bad authenticate message"}
					return
				}
				stage = 3
				writeProxyResponse(conn, http.StatusOK, "")
				// hold tunnel open until client closes
				_, _ = bufio.NewReader(conn).Read(make([]byte, 1))
				done <- nil
				return
			default:
				done <- &testErr{"unexpected request"}
				return
			}
		}
	}()

	u := &url.URL{
		Scheme: "http",
		Host:   ln.Addr().String(),
		User:   url.UserPassword("CORP\\alice", "Passw0rd"),
	}
	p, err := New(u.String())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.Dial(ctx, "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if stage != 3 {
		t.Fatalf("stage = %d, want 3", stage)
	}
}

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }

func writeProxyResponse(conn net.Conn, status int, authenticate string) {
	var b strings.Builder
	b.WriteString("HTTP/1.1 ")
	b.WriteString(strconv.Itoa(status))
	b.WriteString(" ")
	b.WriteString(http.StatusText(status))
	b.WriteString("\r\n")
	if authenticate != "" {
		b.WriteString("Proxy-Authenticate: ")
		b.WriteString(authenticate)
		b.WriteString("\r\n")
	}
	b.WriteString("Content-Length: 0\r\n\r\n")
	_, _ = conn.Write([]byte(b.String()))
}
