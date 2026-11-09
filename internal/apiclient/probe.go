// Package apiclient is the minimal client of the firmware's encrypted native
// API that `sil` needs to decide readiness: a real Noise_NNpsk0 handshake with
// the instance key plus a HelloRequest/HelloResponse exchange. Nothing else of
// the API is spoken (no login, no entity listing), and the connection is closed
// right after the HelloResponse.
//
// A bare TCP connect never counts as ready: another process, or another
// instance whose firmware kept running after losing the port race, can answer
// on the port. Only the instance's own firmware knows its key and reports its
// own name and MAC.
package apiclient

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/flynn/noise"

	"github.com/zeroflow/esphome-sil/internal/clock"
)

// AttemptTimeout is the deadline of one probe attempt (dial plus exchange).
const AttemptTimeout = 2 * time.Second

// Kinds of ProbeError.
const (
	KindDial              = "dial"
	KindPlaintextAPI      = "plaintext_api"
	KindHandshakeRejected = "handshake_rejected"
	KindProtocol          = "protocol"
	KindTimeout           = "timeout"
)

const (
	noisePrologue = "NoiseAPIInit\x00\x00"
	keyLen        = 32

	indicatorPlaintext = 0x00
	indicatorNoise     = 0x01

	msgHelloRequest  = 1
	msgHelloResponse = 2

	clientInfo    = "sil"
	apiMajor      = 1
	apiMinor      = 10
	maxFramesRead = 8 // frames to skim for the HelloResponse
)

// Hello is what the firmware reported: the name and MAC of the server hello
// frame (sent before the handshake) and the HelloResponse fields.
type Hello struct {
	ServerName string // from the server hello frame
	MAC        string // from the server hello frame, as the firmware printed it
	APIMajor   uint32
	APIMinor   uint32
	ServerInfo string // HelloResponse.server_info
	Name       string // HelloResponse.name
}

// ProbeError is a failed probe attempt. The key never appears in it.
type ProbeError struct {
	Kind string // dial|plaintext_api|handshake_rejected|protocol|timeout
	Msg  string
}

func (e *ProbeError) Error() string { return e.Kind + ": " + e.Msg }

func perr(kind, format string, args ...any) *ProbeError {
	return &ProbeError{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// Probe performs one attempt against addr (host:port) with the raw 32-byte
// pre-shared key. The attempt has a deadline of AttemptTimeout on clk. A
// cancelled ctx returns ctx's error; every other failure is a *ProbeError.
func Probe(ctx context.Context, addr string, key []byte, clk clock.Clock) (Hello, error) {
	if len(key) != keyLen {
		return Hello{}, perr(KindProtocol, "the API key must decode to %d bytes", keyLen)
	}
	actx, cancel := clock.WithTimeout(ctx, clk, AttemptTimeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(actx, "tcp", addr)
	if err != nil {
		return Hello{}, classify(ctx, actx, err, KindDial)
	}
	defer conn.Close()
	// Cancellation (or the attempt budget) unblocks reads and writes.
	defer context.AfterFunc(actx, func() { _ = conn.Close() })()
	_ = conn.SetDeadline(clk.Deadline(AttemptTimeout))

	h, err := exchange(conn, key)
	if err != nil {
		return Hello{}, classify(ctx, actx, err, KindProtocol)
	}
	return h, nil
}

// classify maps a failure to ctx's error (caller cancelled), a timeout or a
// ProbeError of the given default kind.
func classify(parent, attempt context.Context, err error, kind string) error {
	var pe *ProbeError
	if errors.As(err, &pe) {
		return pe
	}
	if parent.Err() != nil {
		return parent.Err()
	}
	if clock.TimedOut(attempt) || errors.Is(err, os.ErrDeadlineExceeded) {
		return perr(KindTimeout, "no complete answer within %s", AttemptTimeout)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return perr(KindProtocol, "the connection was closed before the exchange finished")
	}
	return perr(kind, "%s", scrub(err))
}

func scrub(err error) string { return strings.TrimSpace(err.Error()) }

// exchange speaks the Noise native-API protocol on conn.
func exchange(conn net.Conn, key []byte) (Hello, error) {
	var h Hello
	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:           noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256),
		Pattern:               noise.HandshakeNN,
		Initiator:             true,
		Prologue:              []byte(noisePrologue),
		PresharedKey:          key,
		PresharedKeyPlacement: 0,
	})
	if err != nil {
		return h, perr(KindProtocol, "cannot set up the handshake: %v", err)
	}
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return h, perr(KindProtocol, "cannot write the handshake message: %v", err)
	}

	// Empty client hello, then the first handshake message (leading 0x00).
	out := []byte{indicatorNoise, 0, 0}
	out = append(out, frame(append([]byte{0x00}, msg1...))...)
	if _, err := conn.Write(out); err != nil {
		return h, err
	}

	// Server hello: 0x01 name\0 mac\0 (a leading indicator 0x00 is the
	// plaintext API).
	hello, err := readFrame(conn)
	if err != nil {
		return h, err
	}
	if len(hello) < 1 || hello[0] != indicatorNoise {
		return h, perr(KindProtocol, "unexpected server hello")
	}
	parts := strings.Split(string(hello[1:]), "\x00")
	if len(parts) < 2 {
		return h, perr(KindProtocol, "malformed server hello")
	}
	h.ServerName, h.MAC = parts[0], parts[1]

	// Handshake answer: 0x00 + noise message, or 0x01 + error text.
	hf, err := readFrame(conn)
	if err != nil {
		return h, err
	}
	if len(hf) < 1 {
		return h, perr(KindProtocol, "empty handshake frame")
	}
	if hf[0] != 0x00 {
		return h, perr(KindHandshakeRejected, "the firmware rejected the handshake: %s", strings.TrimSpace(string(hf[1:])))
	}
	_, csW, csR, err := hs.ReadMessage(nil, hf[1:])
	if err != nil || csW == nil || csR == nil {
		return h, perr(KindHandshakeRejected, "the handshake did not complete: %v", err)
	}

	// Encrypted HelloRequest.
	req := []byte{0x0a, byte(len(clientInfo))}
	req = append(req, clientInfo...)
	req = append(req, 0x10, apiMajor, 0x18, apiMinor)
	plain := make([]byte, 4, 4+len(req))
	binary.BigEndian.PutUint16(plain[0:], msgHelloRequest)
	binary.BigEndian.PutUint16(plain[2:], uint16(len(req)))
	plain = append(plain, req...)
	ct, err := csW.Encrypt(nil, nil, plain)
	if err != nil {
		return h, perr(KindProtocol, "cannot encrypt the hello request: %v", err)
	}
	if _, err := conn.Write(frame(ct)); err != nil {
		return h, err
	}

	for range maxFramesRead {
		enc, err := readFrame(conn)
		if err != nil {
			return h, err
		}
		dec, err := csR.Decrypt(nil, nil, enc)
		if err != nil {
			return h, perr(KindProtocol, "cannot decrypt a server message: %v", err)
		}
		if len(dec) < 4 {
			return h, perr(KindProtocol, "short server message")
		}
		typ, n := binary.BigEndian.Uint16(dec[0:]), int(binary.BigEndian.Uint16(dec[2:]))
		if 4+n > len(dec) {
			return h, perr(KindProtocol, "truncated server message")
		}
		if typ != msgHelloResponse {
			continue
		}
		if err := parseHelloResponse(dec[4:4+n], &h); err != nil {
			return h, err
		}
		return h, nil
	}
	return h, perr(KindProtocol, "no HelloResponse from the server")
}

// frame wraps payload as a noise frame: indicator 0x01, 2 byte big-endian
// length, payload.
func frame(payload []byte) []byte {
	out := make([]byte, 3, 3+len(payload))
	out[0] = indicatorNoise
	binary.BigEndian.PutUint16(out[1:], uint16(len(payload)))
	return append(out, payload...)
}

// readFrame reads one noise frame: indicator 0x01, 2 byte big-endian length,
// payload. An indicator of 0x00 is the plaintext API.
func readFrame(r io.Reader) ([]byte, error) {
	var hdr [3]byte
	if _, err := io.ReadFull(r, hdr[:1]); err != nil {
		return nil, err
	}
	switch hdr[0] {
	case indicatorNoise:
	case indicatorPlaintext:
		return nil, perr(KindPlaintextAPI, "the port answers the plaintext API (no encryption)")
	default:
		return nil, perr(KindProtocol, "unexpected frame indicator 0x%02x", hdr[0])
	}
	if _, err := io.ReadFull(r, hdr[1:]); err != nil {
		return nil, err
	}
	payload := make([]byte, binary.BigEndian.Uint16(hdr[1:]))
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// parseHelloResponse reads fields 1 major, 2 minor (varints) and 3
// server_info, 4 name (strings); other fields are skipped.
func parseHelloResponse(b []byte, h *Hello) error {
	bad := func() error { return perr(KindProtocol, "malformed HelloResponse") }
	for len(b) > 0 {
		tag, n := uvarint(b)
		if n == 0 {
			return bad()
		}
		b = b[n:]
		field, wire := tag>>3, tag&7
		switch wire {
		case 0:
			v, n := uvarint(b)
			if n == 0 {
				return bad()
			}
			b = b[n:]
			switch field {
			case 1:
				h.APIMajor = uint32(v)
			case 2:
				h.APIMinor = uint32(v)
			}
		case 2:
			l, n := uvarint(b)
			if n == 0 || uint64(len(b)-n) < l {
				return bad()
			}
			s := b[n : n+int(l)]
			b = b[n+int(l):]
			switch field {
			case 3:
				h.ServerInfo = string(s)
			case 4:
				h.Name = string(s)
			}
		case 1:
			if len(b) < 8 {
				return bad()
			}
			b = b[8:]
		case 5:
			if len(b) < 4 {
				return bad()
			}
			b = b[4:]
		default:
			return bad()
		}
	}
	return nil
}

// uvarint decodes a protobuf varint; n == 0 means malformed or short.
func uvarint(b []byte) (v uint64, n int) {
	var shift uint
	for i, c := range b {
		if i >= 10 {
			return 0, 0
		}
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, i + 1
		}
		shift += 7
	}
	return 0, 0
}
