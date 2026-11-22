package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"crypto/hmac"
	"crypto/sha256"
)

type keyTunnel struct {
	conn net.Conn
	aead cipher.AEAD
	rbuf []byte
	wctr uint64
	rctr uint64
}

func WrapKey(conn net.Conn, key []byte) (net.Conn, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &keyTunnel{conn: conn, aead: aead}, nil
}

func (t *keyTunnel) Read(p []byte) (int, error) {
	if t.rctr == ^uint64(0) {
		return 0, fmt.Errorf("crypto: nonce exhausted")
	}
	if len(t.rbuf) == 0 {
		hdr := make([]byte, 2)
		if _, err := io.ReadFull(t.conn, hdr); err != nil {
			return 0, err
		}
		n := int(binary.BigEndian.Uint16(hdr))
		if n == 0 {
			return 0, io.EOF
		}
		ct := make([]byte, n)
		if _, err := io.ReadFull(t.conn, ct); err != nil {
			return 0, err
		}
		nonce := make([]byte, 12)
		binary.LittleEndian.PutUint64(nonce[4:], t.rctr)
		pt, err := t.aead.Open(nil, nonce, ct, nil)
		if err != nil {
			return 0, err
		}
		t.rbuf = pt
		t.rctr++
	}
	n := copy(p, t.rbuf)
	t.rbuf = t.rbuf[n:]
	return n, nil
}

func (t *keyTunnel) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		if t.wctr == ^uint64(0) {
			return total, fmt.Errorf("crypto: nonce exhausted")
		}
		chunk := p
		if len(chunk) > 1<<15 {
			chunk = p[:1<<15]
		}
		nonce := make([]byte, 12)
		binary.LittleEndian.PutUint64(nonce[4:], t.wctr)
		ct := t.aead.Seal(nil, nonce, chunk, nil)
		hdr := make([]byte, 2)
		binary.BigEndian.PutUint16(hdr, uint16(len(ct)))
		if _, err := t.conn.Write(append(hdr, ct...)); err != nil {
			return total, err
		}
		total += len(chunk)
		p = p[len(chunk):]
		t.wctr++
	}
	return total, nil
}

func (t *keyTunnel) Close() error                       { return t.conn.Close() }
func (t *keyTunnel) LocalAddr() net.Addr                { return t.conn.LocalAddr() }
func (t *keyTunnel) RemoteAddr() net.Addr               { return t.conn.RemoteAddr() }
func (t *keyTunnel) SetDeadline(d time.Time) error      { return t.conn.SetDeadline(d) }
func (t *keyTunnel) SetReadDeadline(d time.Time) error  { return t.conn.SetReadDeadline(d) }
func (t *keyTunnel) SetWriteDeadline(d time.Time) error { return t.conn.SetWriteDeadline(d) }

// WrapLayers applies layered encryption (onion-style) n times.
func WrapLayers(conn net.Conn, initiator bool, layers int) (net.Conn, error) {
	c := conn
	for i := 0; i < layers; i++ {
		nt, err := Wrap(c, initiator)
		if err != nil {
			return nil, err
		}
		c = nt
	}
	return c, nil
}

// DeriveKey derives a 32-byte key from shared secret + label.
func DeriveKey(shared []byte, label string) []byte {
	key := make([]byte, 32)
	prk := hmac.New(sha256.New, make([]byte, sha256.Size))
	prk.Write(shared)
	prkBytes := prk.Sum(nil)
	tt := hmac.New(sha256.New, prkBytes)
	tt.Write([]byte(label))
	tt.Write([]byte{1})
	copy(key, tt.Sum(nil))
	return key
}

// WrapPadded wraps with fixed-size frames (constant traffic shape).
func WrapPadded(conn net.Conn, initiator bool, frameSize int) (net.Conn, error) {
	t, err := Wrap(conn, initiator)
	if err != nil {
		return nil, err
	}
	return &paddedConn{Conn: t, frameSize: frameSize}, nil
}

type paddedConn struct {
	net.Conn
	frameSize int
}

func (p *paddedConn) Write(b []byte) (int, error) {
	orig := len(b)
	if p.frameSize > 0 && len(b) < p.frameSize {
		pad := make([]byte, p.frameSize-len(b))
		// random padding to avoid leaking length shape
		rand.Read(pad)
		b = append(append([]byte{}, b...), pad...)
	}
	n, err := p.Conn.Write(b)
	if err != nil {
		if n > orig {
			n = orig
		}
		return n, err
	}
	if n > orig {
		n = orig
	}
	return n, nil
}

// Rekey creates a new tunnel over conn using newKey.
func Rekey(conn net.Conn, newKey []byte) (net.Conn, error) {
	return WrapKey(conn, newKey)
}

// OnionKeyExchange performs X25519 key exchange for one layer.
func OnionKeyExchange() (priv *ecdh.PrivateKey, pub []byte, err error) {
	curve := ecdh.X25519()
	k, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return k, k.PublicKey().Bytes(), nil
}

var _ = fmt.Errorf
var _ = io.EOF
var _ = ecdh.X25519
