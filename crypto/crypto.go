package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

const maxFrame = 1 << 16

type tunnel struct {
	conn net.Conn
	aead cipher.AEAD
	rbuf []byte
	wctr uint64
	rctr uint64
}

func Wrap(conn net.Conn, initiator bool) (net.Conn, error) {
	return WrapContext(context.Background(), conn, initiator)
}

func WrapContext(ctx context.Context, conn net.Conn, initiator bool) (net.Conn, error) {
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
		defer conn.SetDeadline(time.Time{})
	}
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	pub := priv.PublicKey().Bytes()
	var peerPub []byte
	if initiator {
		if _, err := conn.Write(pub); err != nil {
			return nil, err
		}
		peerPub = make([]byte, 32)
		if _, err := io.ReadFull(conn, peerPub); err != nil {
			return nil, err
		}
	} else {
		peerPub = make([]byte, 32)
		if _, err := io.ReadFull(conn, peerPub); err != nil {
			return nil, err
		}
		if _, err := conn.Write(pub); err != nil {
			return nil, err
		}
	}
	peer, err := curve.NewPublicKey(peerPub)
	if err != nil {
		return nil, err
	}
	shared, err := priv.ECDH(peer)
	if err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	// HKDF-Extract + Expand (RFC 5869), salt=nil -> zeros
	prk := hmac.New(sha256.New, make([]byte, sha256.Size))
	prk.Write(shared)
	prkBytes := prk.Sum(nil)
	// expand
	tt := hmac.New(sha256.New, prkBytes)
	tt.Write([]byte("proxychain-tunnel"))
	tt.Write([]byte{1})
	copy(key, tt.Sum(nil))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &tunnel{conn: conn, aead: aead}, nil
}

func (t *tunnel) Read(p []byte) (int, error) {
	if t.rctr == ^uint64(0) {
		return 0, fmt.Errorf("crypto: nonce exhausted")
	}
	if len(t.rbuf) == 0 {
		hdr := make([]byte, 2)
		if _, err := io.ReadFull(t.conn, hdr); err != nil {
			return 0, err
		}
		n := int(binary.BigEndian.Uint16(hdr))
		if n == 0 || n > maxFrame {
			return 0, fmt.Errorf("crypto: bad frame size %d", n)
		}
		ct := make([]byte, n)
		if _, err := io.ReadFull(t.conn, ct); err != nil {
			return 0, err
		}
		nonce := t.rnonce()
		pt, err := t.aead.Open(nil, nonce[:], ct, nil)
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

func (t *tunnel) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		if t.wctr == ^uint64(0) {
			return total, fmt.Errorf("crypto: nonce exhausted")
		}
		chunk := p
		if len(chunk) > maxFrame-32 {
			chunk = p[:maxFrame-32]
		}
		nonce := t.wnonce()
		ct := t.aead.Seal(nil, nonce[:], chunk, nil)
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

func (t *tunnel) Close() error                       { return t.conn.Close() }
func (t *tunnel) LocalAddr() net.Addr                { return t.conn.LocalAddr() }
func (t *tunnel) RemoteAddr() net.Addr               { return t.conn.RemoteAddr() }
func (t *tunnel) SetDeadline(d time.Time) error      { return t.conn.SetDeadline(d) }
func (t *tunnel) SetReadDeadline(d time.Time) error  { return t.conn.SetReadDeadline(d) }
func (t *tunnel) SetWriteDeadline(d time.Time) error { return t.conn.SetWriteDeadline(d) }

func (t *tunnel) wnonce() [12]byte {
	var n [12]byte
	binary.LittleEndian.PutUint64(n[4:], t.wctr)
	return n
}
func (t *tunnel) rnonce() [12]byte {
	var n [12]byte
	binary.LittleEndian.PutUint64(n[4:], t.rctr)
	return n
}
