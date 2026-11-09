package apiclient

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/flynn/noise"

	"github.com/zeroflow/esphome-sil/internal/clock"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

type responder struct {
	key       []byte
	name, mac string
	mode      string // "", "plaintext", "silent", "badframe"
}

// serve starts the fake firmware and returns its address.
func (r responder) serve(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r.handle(c)
			}()
		}
	}()
	return ln.Addr().String()
}

func (r responder) handle(c net.Conn) {
	switch r.mode {
	case "plaintext":
		_, _ = c.Write([]byte{0x00, 0x00, 0x00})
		return
	case "silent":
		_, _ = io.Copy(io.Discard, c) // accept and never write
		return
	case "badframe":
		_, _ = c.Write([]byte{0x07, 0, 0})
		return
	}
	var empty [3]byte
	if _, err := io.ReadFull(c, empty[:]); err != nil || empty != [3]byte{1, 0, 0} {
		return
	}
	in, err := readFrame(c)
	if err != nil || len(in) < 1 || in[0] != 0 {
		return
	}
	_, _ = c.Write(frame(append([]byte{1}, []byte(r.name+"\x00"+r.mac+"\x00")...)))
	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite: noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256),
		Pattern:     noise.HandshakeNN, Initiator: false,
		Prologue: []byte(noisePrologue), PresharedKey: r.key, PresharedKeyPlacement: 0,
	})
	if err != nil {
		return
	}
	if _, _, _, err := hs.ReadMessage(nil, in[1:]); err != nil {
		_, _ = c.Write(frame(append([]byte{1}, "Handshake MAC failure"...)))
		return
	}
	msg2, csR, csW, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return
	}
	_, _ = c.Write(frame(append([]byte{0}, msg2...)))
	enc, err := readFrame(c)
	if err != nil {
		return
	}
	dec, err := csR.Decrypt(nil, nil, enc)
	if err != nil || len(dec) < 4 || binary.BigEndian.Uint16(dec) != 1 {
		return
	}
	// HelloResponse: major 1, minor 19, server_info, name.
	body := []byte{0x08, 1, 0x10, 19, 0x1a, 9}
	body = append(body, "2026.10.1"...)
	body = append(body, 0x22, byte(len(r.name)))
	body = append(body, r.name...)
	pt := []byte{0, 2, 0, byte(len(body))}
	pt = append(pt, body...)
	ct, _ := csW.Encrypt(nil, nil, pt)
	_, _ = c.Write(frame(ct))
}

func probe(t *testing.T, addr string, key []byte) (Hello, error) {
	t.Helper()
	return Probe(context.Background(), addr, key, clock.NewReal())
}

func kindOf(err error) string {
	var pe *ProbeError
	if errors.As(err, &pe) {
		return pe.Kind
	}
	return ""
}

func TestProbeSuccess(t *testing.T) {
	addr := responder{key: testKey, name: "sil-a", mac: "02aaaaaaaa01"}.serve(t)
	h, err := probe(t, addr, testKey)
	if err != nil {
		t.Fatal(err)
	}
	want := Hello{ServerName: "sil-a", MAC: "02aaaaaaaa01", APIMajor: 1, APIMinor: 19, ServerInfo: "2026.10.1", Name: "sil-a"}
	if h != want {
		t.Errorf("hello = %+v, want %+v", h, want)
	}
}

func TestProbeWrongKey(t *testing.T) {
	addr := responder{key: testKey, name: "sil-a", mac: "02aaaaaaaa01"}.serve(t)
	wrong := []byte("ffffffffffffffffffffffffffffffff")
	_, err := probe(t, addr, wrong)
	if kindOf(err) != KindHandshakeRejected || !strings.Contains(err.Error(), "Handshake MAC failure") {
		t.Fatalf("err = %v, want handshake_rejected with the server text", err)
	}
	if strings.Contains(err.Error(), string(wrong)) {
		t.Errorf("the key leaked into the error: %v", err)
	}
}

func TestProbePlaintextAPI(t *testing.T) {
	addr := responder{mode: "plaintext"}.serve(t)
	if _, err := probe(t, addr, testKey); kindOf(err) != KindPlaintextAPI {
		t.Fatalf("err = %v, want plaintext_api", err)
	}
}

func TestProbeBadIndicator(t *testing.T) {
	addr := responder{mode: "badframe"}.serve(t)
	if _, err := probe(t, addr, testKey); kindOf(err) != KindProtocol {
		t.Fatalf("err = %v, want protocol", err)
	}
}

func TestProbeSilentServerTimesOut(t *testing.T) {
	clk := clock.NewReal()
	addr := responder{mode: "silent"}.serve(t)
	start := clk.Now()
	_, err := Probe(context.Background(), addr, testKey, clk)
	took := clk.Now() - start
	if kindOf(err) != KindTimeout {
		t.Fatalf("err = %v, want timeout", err)
	}
	if took < AttemptTimeout-100e6 || took > AttemptTimeout+1500e6 {
		t.Errorf("timeout took %s, want about %s", took, AttemptTimeout)
	}
}

func TestProbeClosedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	if _, err := probe(t, addr, testKey); kindOf(err) != KindDial {
		t.Fatalf("err = %v, want dial", err)
	}
}

func TestProbeCancelled(t *testing.T) {
	addr := responder{mode: "silent"}.serve(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Probe(ctx, addr, testKey, clock.NewReal()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestProbeRejectsShortKey(t *testing.T) {
	if _, err := probe(t, "127.0.0.1:1", []byte("short")); kindOf(err) != KindProtocol {
		t.Fatalf("err = %v, want protocol", err)
	}
}

func TestParseHelloResponseSkipsUnknownFields(t *testing.T) {
	// field 9 varint, field 10 length-delimited, then name (4).
	b := []byte{0x48, 0x05, 0x52, 0x02, 'x', 'y', 0x22, 0x03, 'a', 'b', 'c'}
	var h Hello
	if err := parseHelloResponse(b, &h); err != nil || h.Name != "abc" {
		t.Fatalf("h = %+v err = %v", h, err)
	}
	for _, bad := range [][]byte{{0x22}, {0x22, 0x05, 'a'}, {0x08}, {0x23}} {
		if err := parseHelloResponse(bad, &Hello{}); kindOf(err) != KindProtocol {
			t.Errorf("parse(%x) = %v, want protocol", bad, err)
		}
	}
}
