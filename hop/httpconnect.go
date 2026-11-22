package hop

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/Azure/go-ntlmssp"
	utls "github.com/refraction-networking/utls"
)

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }

func dialHTTPConnect(ctx context.Context, p *Proxy, network, addr string) (net.Conn, error) {
	proxyAddr := p.URL.Host
	if _, _, err := net.SplitHostPort(proxyAddr); err != nil {
		port := "80"
		if p.URL.Scheme == "https" {
			port = "443"
		}
		proxyAddr = net.JoinHostPort(proxyAddr, port)
	}

	var conn net.Conn
	var err error
	if p.URL.Scheme == "https" {
		host := p.URL.Hostname()
		tcpConn, err := p.dialTCP(ctx, "tcp", proxyAddr)
		if err != nil {
			return nil, err
		}
		uconn := utls.UClient(tcpConn, &utls.Config{ServerName: host}, utls.HelloRandomized)
		if err := uconn.HandshakeContext(ctx); err != nil {
			tcpConn.Close()
			return nil, err
		}
		conn = uconn
	} else {
		conn, err = p.dialTCP(ctx, "tcp", proxyAddr)
	}
	if err != nil {
		return nil, err
	}
	c, err := httpConnect(conn, addr, p)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

// httpConnect performs an HTTP CONNECT to target through proxy p over conn.
func httpConnect(conn net.Conn, target string, p *Proxy) (net.Conn, error) {
	br := bufio.NewReader(conn)
	// first attempt: Basic auth if credentials present
	auth := ""
	if u := p.URL.User; u != nil {
		pass, _ := u.Password()
		cred := u.Username() + ":" + pass
		auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(cred))
	}
	resp, err := writeConnect(conn, br, target, auth)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	// challenge handling
	if resp.StatusCode == http.StatusProxyAuthRequired {
		challenge := strings.Join(resp.Header.Values("Proxy-Authenticate"), ", ")
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		lower := strings.ToLower(challenge)
		if p.URL.User != nil {
			if strings.Contains(lower, "digest") {
				u := p.URL.User
				pass, _ := u.Password()
				digestAuth := computeDigest(challenge, u.Username(), pass, "CONNECT", target)
				resp2, err := writeConnect(conn, br, target, digestAuth)
				if err != nil {
					return nil, err
				}
				io.Copy(io.Discard, resp2.Body)
				resp2.Body.Close()
				if resp2.StatusCode == http.StatusOK {
					return &bufferedConn{Conn: conn, r: br}, nil
				}
				return nil, fmt.Errorf("hop: proxy connect %s", resp2.Status)
			}
			if strings.Contains(lower, "ntlm") {
				return ntlmConnect(conn, br, target, p)
			}
		}
		return nil, fmt.Errorf("hop: proxy auth required")
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return nil, fmt.Errorf("hop: proxy connect %s", resp.Status)
}

// ntlmConnect performs the three-message NTLMv2 challenge/response exchange.
func ntlmConnect(conn net.Conn, br *bufio.Reader, target string, p *Proxy) (net.Conn, error) {
	negotiate, err := ntlmssp.NewNegotiateMessage("", "")
	if err != nil {
		return nil, err
	}
	resp, err := writeConnect(conn, br, target, "NTLM "+base64.StdEncoding.EncodeToString(negotiate))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusProxyAuthRequired {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return &bufferedConn{Conn: conn, r: br}, nil
		}
		return nil, fmt.Errorf("hop: proxy connect %s", resp.Status)
	}
	challengeMsg, ok := extractNTLMChallenge(resp.Header)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if !ok {
		return nil, fmt.Errorf("hop: missing NTLM challenge")
	}
	u := p.URL.User
	pass, _ := u.Password()
	auth, err := ntlmssp.NewAuthenticateMessage(challengeMsg, u.Username(), pass, nil)
	if err != nil {
		return nil, err
	}
	resp2, err := writeConnect(conn, br, target, "NTLM "+base64.StdEncoding.EncodeToString(auth))
	if err != nil {
		return nil, err
	}
	io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode == http.StatusOK {
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return nil, fmt.Errorf("hop: proxy connect %s", resp2.Status)
}

// extractNTLMChallenge returns the NTLM challenge token from Proxy-Authenticate.
func extractNTLMChallenge(h http.Header) ([]byte, bool) {
	for _, v := range h.Values("Proxy-Authenticate") {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if len(part) <= 5 || !strings.EqualFold(part[:5], "NTLM ") {
				continue
			}
			tok := strings.TrimSpace(part[5:])
			if tok == "" {
				continue
			}
			b, err := base64.StdEncoding.DecodeString(tok)
			if err == nil && len(b) > 0 {
				return b, true
			}
		}
	}
	return nil, false
}

func writeConnect(conn net.Conn, br *bufio.Reader, target, auth string) (*http.Response, error) {
	req := &http.Request{
		Method:     "CONNECT",
		URL:        &url.URL{Opaque: target},
		Host:       target,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
	}
	if auth != "" {
		req.Header.Set("Proxy-Authorization", auth)
	}
	if err := req.Write(conn); err != nil {
		return nil, err
	}
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func computeDigest(challenge, username, password, method, uri string) string {
	params := parseDigestParams(challenge)
	realm := params["realm"]
	nonce := params["nonce"]
	qop := params["qop"]
	opaque := params["opaque"]
	algorithm := params["algorithm"]
	if algorithm == "" {
		algorithm = "MD5"
	}
	ha1 := md5Hex(username + ":" + realm + ":" + password)
	ha2 := md5Hex(method + ":" + uri)
	var cnonce string
	var nc string
	if qop != "" {
		cnonce = randomHex(8)
		nc = "00000001"
	}
	var response string
	if qop != "" {
		response = md5Hex(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = md5Hex(ha1 + ":" + nonce + ":" + ha2)
	}
	parts := []string{
		`username="` + username + `"`,
		`realm="` + realm + `"`,
		`nonce="` + nonce + `"`,
		`uri="` + uri + `"`,
		`response="` + response + `"`,
	}
	if opaque != "" {
		parts = append(parts, `opaque="`+opaque+`"`)
	}
	if algorithm != "" {
		parts = append(parts, `algorithm=`+algorithm)
	}
	if qop != "" {
		parts = append(parts, `qop=`+qop, `nc=`+nc, `cnonce="`+cnonce+`"`)
	}
	return "Digest " + strings.Join(parts, ", ")
}

func parseDigestParams(challenge string) map[string]string {
	m := map[string]string{}
	// strip "Digest"
	s := challenge
	if i := strings.Index(strings.ToLower(s), "digest"); i >= 0 {
		s = s[i+len("digest"):]
	}
	// parse key=value pairs
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if eq := strings.Index(part, "="); eq > 0 {
			k := strings.TrimSpace(part[:eq])
			v := strings.TrimSpace(part[eq+1:])
			v = strings.Trim(v, `"`)
			m[strings.ToLower(k)] = v
		}
	}
	return m
}

func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
