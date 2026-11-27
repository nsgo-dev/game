package netplay

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/nsgo-dev/game/pkg/debug"
)

// TestNoiseNKMatchesSpecStepByStep independently recomputes every Noise_NK_25519_ChaChaPoly_SHA256 Revision 34 step from raw RFC 7748 X25519, RFC 8439 ChaCha20-Poly1305, and SHA-256/HMAC-SHA256 equations, confirming exact protocol conformance and transcript binding.
func TestNoiseNKMatchesSpecStepByStep(t *testing.T) {
	mustHex := func(s string) []byte {
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	// Fixed X25519 private keys from Noise/RFC 7748 test vectors.
	rsPriv, err := ecdh.X25519().NewPrivateKey(mustHex("4a3acbfdb163dec651dfa3194dece676d437029c62a408b4c5ea9114246e4893"))
	if err != nil {
		t.Fatal(err)
	}
	iePriv, err := ecdh.X25519().NewPrivateKey(mustHex("893e28b9dc6ca8d611ab664754b8ceb7bac5117349a4439a6b0569da977c464a"))
	if err != nil {
		t.Fatal(err)
	}
	rePriv, err := ecdh.X25519().NewPrivateKey(mustHex("bbdb4cdbd309f1a1f2e1456967fe288cadd6f712d65dc7b7793d5e63da6b375b"))
	if err != nil {
		t.Fatal(err)
	}
	prologue := []byte("Prologue123")
	payload1 := []byte("Ludicrous")
	payload2 := []byte("Speed")

	// Independent Noise Rev 34 reference calculation.
	var refH, refCK [32]byte
	copy(refH[:], "Noise_NK_25519_ChaChaPoly_SHA256")
	refCK = refH
	mixHash := func(d []byte) {
		h := sha256.New()
		h.Write(refH[:])
		h.Write(d)
		h.Sum(refH[:0])
	}
	hkdf2 := func(ck [32]byte, ikm []byte) (o1, o2 [32]byte) {
		var tmp [32]byte
		m := hmac.New(sha256.New, ck[:])
		m.Write(ikm)
		m.Sum(tmp[:0])
		m1 := hmac.New(sha256.New, tmp[:])
		m1.Write([]byte{1})
		m1.Sum(o1[:0])
		m2 := hmac.New(sha256.New, tmp[:])
		m2.Write(o1[:])
		m2.Write([]byte{2})
		m2.Sum(o2[:0])
		return o1, o2
	}
	sealNoise := func(key [32]byte, n uint64, ad, pt []byte) []byte {
		aead, _ := chacha20poly1305.New(key[:])
		var nonce [12]byte
		binary.LittleEndian.PutUint64(nonce[4:12], n)
		return aead.Seal(nil, nonce[:], pt, ad)
	}

	mixHash(prologue)
	mixHash(rsPriv.PublicKey().Bytes())
	// Msg 1: -> e, es
	mixHash(iePriv.PublicKey().Bytes())
	es, _ := iePriv.ECDH(rsPriv.PublicKey())
	var k1 [32]byte
	refCK, k1 = hkdf2(refCK, es)
	ct1 := sealNoise(k1, 0, refH[:], payload1)
	mixHash(ct1)
	wantMsg1 := append(append([]byte(nil), iePriv.PublicKey().Bytes()...), ct1...)

	// Msg 2: <- e, ee
	mixHash(rePriv.PublicKey().Bytes())
	ee, _ := rePriv.ECDH(iePriv.PublicKey())
	var k2 [32]byte
	refCK, k2 = hkdf2(refCK, ee)
	ct2 := sealNoise(k2, 0, refH[:], payload2)
	mixHash(ct2)
	wantMsg2 := append(append([]byte(nil), rePriv.PublicKey().Bytes()...), ct2...)
	wantC2S, wantS2C := hkdf2(refCK, nil)

	// Run our implementation.
	init, err := newNoiseNKInitiator(prologue, rsPriv.PublicKey(), iePriv)
	if err != nil {
		t.Fatalf("newNoiseNKInitiator: %v", err)
	}
	msg1, err := init.writeMessage1(payload1)
	if err != nil {
		t.Fatalf("writeMessage1: %v", err)
	}
	if !bytes.Equal(msg1, wantMsg1) {
		t.Fatalf("msg1 mismatch:\n got %x\nwant %x", msg1, wantMsg1)
	}

	resp, err := newNoiseNKResponder(prologue, rsPriv)
	if err != nil {
		t.Fatalf("newNoiseNKResponder: %v", err)
	}
	got1, err := resp.readMessage1(msg1)
	if err != nil || !bytes.Equal(got1, payload1) {
		t.Fatalf("readMessage1 = %q, %v; want %q", got1, err, payload1)
	}
	msg2, respC2S, respS2C, err := resp.writeMessage2(payload2, rePriv)
	if err != nil {
		t.Fatalf("writeMessage2: %v", err)
	}
	if !bytes.Equal(msg2, wantMsg2) {
		t.Fatalf("msg2 mismatch:\n got %x\nwant %x", msg2, wantMsg2)
	}

	// A forged message 2 must fail without corrupting the initiator's state.
	forged := bytes.Clone(msg2)
	forged[len(forged)-1] ^= 0x01
	if _, _, _, err := init.readMessage2(forged); err == nil {
		t.Fatal("initiator accepted a tampered message 2")
	}

	got2, initC2S, initS2C, err := init.readMessage2(msg2)
	if err != nil || !bytes.Equal(got2, payload2) {
		t.Fatalf("readMessage2 = %q, %v; want %q", got2, err, payload2)
	}
	if initC2S != wantC2S || respC2S != wantC2S || initS2C != wantS2C || respS2C != wantS2C {
		t.Fatalf("split keys mismatch:\ninit=(%x, %x)\nresp=(%x, %x)\nwant=(%x, %x)",
			initC2S, initS2C, respC2S, respS2C, wantC2S, wantS2C)
	}
	if initC2S == initS2C {
		t.Fatal("directional keys must differ")
	}
	if init.ss.h != refH || resp.ss.h != refH {
		t.Fatalf("final handshake hash mismatch: init=%x resp=%x want=%x", init.ss.h, resp.ss.h, refH)
	}
}

func TestUDPHandshakeV2RoundTripAndAmplificationBounds(t *testing.T) {
	srvKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	token := []byte("secret-auth-token-1234567890")
	var secret [hsSecretSize]byte
	copy(secret[:], "resume-secret-01")
	h, err := newHello(token, resumeState{peer: 42, secret: secret})
	if err != nil {
		t.Fatal(err)
	}
	dgram, init, err := encodeUDPHelloV2(srvKey.PublicKey(), h)
	if err != nil {
		t.Fatalf("encodeUDPHelloV2: %v", err)
	}
	if len(dgram) < udpMinHello {
		t.Fatalf("hello = %d bytes, want at least %d", len(dgram), udpMinHello)
	}
	// Neither the token nor the resume secret may appear in cleartext on the wire.
	if bytes.Contains(dgram, token) {
		t.Fatal("hello leaked token in plaintext")
	}
	if bytes.Contains(dgram, secret[:]) {
		t.Fatal("hello leaked resume secret in plaintext")
	}

	resp, decoded, err := decodeUDPHelloV2(srvKey, dgram)
	if err != nil {
		t.Fatalf("decodeUDPHelloV2: %v", err)
	}
	if decoded.nonce != h.nonce || decoded.resumePeer != 42 || decoded.resumeSecret != secret || !bytes.Equal(decoded.token, token) {
		t.Fatalf("decoded hello = %+v, want match", decoded)
	}

	var newSecret [hsSecretSize]byte
	copy(newSecret[:], "fresh-secret-002")
	w := welcome{nonce: h.nonce, peer: 7, server: 1, resumed: true, secret: newSecret}
	wDgram, srvC2S, srvS2C, err := encodeUDPWelcomeV2(resp, w)
	if err != nil {
		t.Fatalf("encodeUDPWelcomeV2: %v", err)
	}
	if len(wDgram) != udpWelcomeSize || len(wDgram) > udpMinHello {
		t.Fatalf("welcome = %d bytes, want %d (<= %d)", len(wDgram), udpWelcomeSize, udpMinHello)
	}
	if bytes.Contains(wDgram, newSecret[:]) {
		t.Fatal("welcome leaked resume secret in plaintext")
	}

	gotW, cliC2S, cliS2C, err := acceptUDPServerHandshakeV2(init, h.nonce, wDgram)
	if err != nil {
		t.Fatalf("acceptUDPServerHandshakeV2: %v", err)
	}
	if gotW != w || cliC2S != srvC2S || cliS2C != srvS2C {
		t.Fatalf("welcome or keys mismatch: gotW=%+v want=%+v", gotW, w)
	}
}

func TestUDPHandshakeV2RejectAndTamper(t *testing.T) {
	srvKey, _ := ecdh.X25519().GenerateKey(rand.Reader)
	wrongKey, _ := ecdh.X25519().GenerateKey(rand.Reader)

	h, _ := newHello([]byte("bad-token"), resumeState{})
	dgram, init, err := encodeUDPHelloV2(srvKey.PublicKey(), h)
	if err != nil {
		t.Fatal(err)
	}
	// Wrong server key must fail authentication.
	if _, _, err := decodeUDPHelloV2(wrongKey, dgram); err == nil {
		t.Fatal("wrong server key decrypted hello")
	}
	// Flipping any byte of the hello (header, nonce, ephemeral key, ciphertext or tag) must fail.
	for i := range dgram {
		tampered := bytes.Clone(dgram)
		tampered[i] ^= 0x01
		if _, _, err := decodeUDPHelloV2(srvKey, tampered); err == nil {
			t.Fatalf("hello accepted with byte %d tampered", i)
		}
	}

	resp, _, err := decodeUDPHelloV2(srvKey, dgram)
	if err != nil {
		t.Fatal(err)
	}
	rejDgram, err := encodeUDPRejectV2(resp, h.nonce, "bad token", len(dgram))
	if err != nil {
		t.Fatal(err)
	}
	if len(rejDgram) > len(dgram) {
		t.Fatalf("reject (%d) outgrew hello (%d)", len(rejDgram), len(dgram))
	}
	if bytes.Contains(rejDgram, []byte("bad token")) {
		t.Fatal("reject leaked reason in plaintext")
	}
	// Flipping any byte of the reject must fail (including flipping hsReject -> hsWelcome).
	for i := range rejDgram {
		tampered := bytes.Clone(rejDgram)
		tampered[i] ^= 0x01
		if _, _, _, err := acceptUDPServerHandshakeV2(init, h.nonce, tampered); !errors.Is(err, ErrInvalidPacket) {
			t.Fatalf("reject accepted with byte %d tampered: %v", i, err)
		}
	}
	_, _, _, err = acceptUDPServerHandshakeV2(init, h.nonce, rejDgram)
	var rej *RejectError
	if !errors.As(err, &rej) || rej.Reason != "bad token" || !errors.Is(err, ErrRejected) {
		t.Fatalf("reject = %v, want *RejectError{bad token}", err)
	}
}

func TestUDPCipherSealOpenZeroAllocs(t *testing.T) {
	key := [32]byte{1, 2, 3, 4, 5, 6, 7, 8}
	sealer := newUDPCipher(key)
	opener := newUDPCipher(key)
	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}
	dgram := make([]byte, 0, udpSealedHeader+len(payload))
	plain := make([]byte, 0, len(payload))
	var seq uint32

	debug.AssertZeroAllocs(t, 1000, func() {
		seq++
		dgram = sealer.seal(dgram[:0], udpDirClientToServer, udpData, 2, seq, ChannelUnreliable, payload)
	})
	if len(dgram) != udpSealedHeader+len(payload) {
		t.Fatalf("sealed len = %d, want %d", len(dgram), udpSealedHeader+len(payload))
	}
	debug.AssertZeroAllocs(t, 1000, func() {
		var ok bool
		plain, ok = opener.open(plain[:0], udpDirClientToServer, dgram)
		if !ok {
			t.Fatal("open failed")
		}
	})
	if !bytes.Equal(plain, payload) {
		t.Fatalf("decrypted payload mismatch")
	}
}

func TestUDPCipherTamperAndDirectionMismatch(t *testing.T) {
	key := [32]byte{7, 8, 9}
	sealer := newUDPCipher(key)
	opener := newUDPCipher(key)
	payload := []byte("fog-of-war-secret-units")
	dgram := sealer.seal(nil, udpDirClientToServer, udpData, 5, 1, ChannelUnreliable, payload)
	if len(dgram) != udpSealedHeader+len(payload) {
		t.Fatalf("sealed len = %d, want %d", len(dgram), udpSealedHeader+len(payload))
	}
	if bytes.Contains(dgram, payload) {
		t.Fatal("sealed datagram leaked plaintext payload")
	}
	// Wrong direction (reflection) must fail even with the same key.
	if _, ok := opener.open(nil, udpDirServerToClient, dgram); ok {
		t.Fatal("opened datagram with opposite direction")
	}
	// Flipping any byte of header (AD), ciphertext, or tag must fail.
	for i := range dgram {
		tampered := bytes.Clone(dgram)
		tampered[i] ^= 0x80
		if _, ok := opener.open(nil, udpDirClientToServer, tampered); ok {
			t.Fatalf("opened datagram with byte %d tampered", i)
		}
	}
	// Truncating below udpSealedHeader must fail.
	if _, ok := opener.open(nil, udpDirClientToServer, dgram[:udpSealedHeader-1]); ok {
		t.Fatal("opened truncated datagram")
	}
}

func TestNextSeqAndReplayWindowWrapRefusal(t *testing.T) {
	seq := uint32(math.MaxUint32 - 1)
	got, ok := nextSeq(&seq)
	if !ok || got != math.MaxUint32 || seq != math.MaxUint32 {
		t.Fatalf("nextSeq near max = %d, %v", got, ok)
	}
	if got, ok := nextSeq(&seq); ok || got != 0 || seq != math.MaxUint32 {
		t.Fatalf("nextSeq at max = %d, %v (seq=%d); want refusal without wrap", got, ok, seq)
	}

	var w replayWindow
	if !w.wouldAccept(math.MaxUint32) || !w.accept(math.MaxUint32) {
		t.Fatal("replayWindow refused MaxUint32")
	}
	// Replaying MaxUint32 or wrapping to 0 or 1 must be refused.
	for _, bad := range []uint32{0, 1, 2, math.MaxUint32} {
		if w.wouldAccept(bad) || w.accept(bad) {
			t.Fatalf("replayWindow accepted %d after MaxUint32", bad)
		}
	}
	// An in-window out-of-order packet before MaxUint32 is still accepted once.
	if !w.wouldAccept(math.MaxUint32-1) || !w.accept(math.MaxUint32-1) {
		t.Fatal("replayWindow refused in-window MaxUint32-1")
	}
	if w.wouldAccept(math.MaxUint32-1) || w.accept(math.MaxUint32-1) {
		t.Fatal("replayWindow accepted duplicate MaxUint32-1")
	}
}

func FuzzDecodeUDPHelloV2(f *testing.F) {
	srvKey, _ := ecdh.X25519().GenerateKey(rand.Reader)
	h, _ := newHello([]byte("tok"), resumeState{peer: 3})
	good, _, _ := encodeUDPHelloV2(srvKey.PublicKey(), h)
	f.Add(good)
	f.Add([]byte{hsHello, udpHandshakeVersion})
	f.Add(encodeHello(h, udpMinHello))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _, _ = decodeUDPHelloV2(srvKey, b)
	})
}
