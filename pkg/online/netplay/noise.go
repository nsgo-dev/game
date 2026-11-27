package netplay

import (
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"slices"

	"golang.org/x/crypto/chacha20poly1305"
)

// Noise Protocol Framework Revision 34 implementation of Noise_NK_25519_ChaChaPoly_SHA256 for UDP handshake version 2, plus per-datagram ChaCha20-Poly1305 AEAD framing.
//
// UDP handshake version 2 wire layout:
//
//	hello:   hsHello(1)   version(1)=2 nonce(16) noiseMsg1(e(32) || AEAD(resumePeer(4) resumeSecret(16) tokenLen(2) token padding))
//	welcome: hsWelcome(1) version(1)=2 nonce(16) noiseMsg2(e(32) || AEAD(hsWelcome(1) peer(4) server(4) resumed(1) secret(16)))
//	reject:  hsReject(1)  version(1)=2 nonce(16) noiseMsg2(e(32) || AEAD(hsReject(1) reasonLen(2) reason))
//
// The 17-byte slice version(1) || nonce(16) is passed as the Noise prologue so both endpoints bind the protocol version and handshake nonce into the transcript hash.
//
// After the handshake, every UDP datagram is sealed with ChaCha20-Poly1305 using the directional keys from Noise Split (or a peer's pre-shared key on UDPTransport):
//
//	type(1) peer(4) seq(4) channel(1) ciphertext(len(payload)) tag(16)
//
// The 10-byte cleartext header is authenticated as associated data, and the 12-byte nonce is implicit: direction(4) || peer(4) || seq(4) in big-endian order.
const (
	noiseNKProtocolName = "Noise_NK_25519_ChaChaPoly_SHA256"

	udpHandshakeVersion = 2
	udpHelloPrefixSize  = 2 + hsNonceSize // type(1) version(1) nonce(16)
	udpHelloBodyHeader  = 4 + hsSecretSize + 2
	udpWelcomeBodySize  = 1 + 4 + 4 + 1 + hsSecretSize
	udpRejectBodyHeader = 1 + 2
	udpNoiseMsgOverhead = 32 + chacha20poly1305.Overhead
	udpMinHelloPlain    = udpMinHello - udpHelloPrefixSize - udpNoiseMsgOverhead
	udpMaxHello         = udpHelloPrefixSize + udpNoiseMsgOverhead + udpHelloBodyHeader + MaxTokenSize
	udpWelcomeSize      = udpHelloPrefixSize + udpNoiseMsgOverhead + udpWelcomeBodySize
	udpRejectOverhead   = udpHelloPrefixSize + udpNoiseMsgOverhead + udpRejectBodyHeader

	udpData       = 0xB1 // client to server
	udpServerData = 0xB2 // server to client
	udpPing       = 0xB3 // client to server keepalive
	udpPong       = 0xB4 // server to client keepalive reply
	udpBye        = 0xB5 // client to server goodbye
	udpPeerData   = 0xB6 // UDPTransport pre-shared key peer datagram

	udpDirClientToServer uint32 = 1
	udpDirServerToClient uint32 = 2

	udpCleartextHeader = 10
	udpTagSize         = chacha20poly1305.Overhead       // 16
	udpSealedHeader    = udpCleartextHeader + udpTagSize // 26
)

var errNoiseAuth = errors.New("net: noise authentication failed")

func validateX25519PrivateKey(k *ecdh.PrivateKey) error {
	if k == nil || k.Curve() != ecdh.X25519() {
		return ErrInvalidKey
	}
	return nil
}

func validateX25519PublicKey(k *ecdh.PublicKey) error {
	if k == nil || k.Curve() != ecdh.X25519() {
		return ErrInvalidKey
	}
	return nil
}

// noiseCipherState implements Noise Protocol Revision 34 §5.1 for ChaChaPoly.
type noiseCipherState struct {
	aead   cipher.AEAD
	n      uint64
	hasKey bool
}

func (c *noiseCipherState) initKey(key [32]byte) {
	aead, err := chacha20poly1305.New(key[:])
	if err != nil {
		panic(err) // key is always 32 bytes
	}
	c.aead = aead
	c.n = 0
	c.hasKey = true
}

func (c *noiseCipherState) encryptWithAd(ad, plaintext []byte) ([]byte, error) {
	if !c.hasKey {
		return slices.Clone(plaintext), nil
	}
	if c.n == math.MaxUint64 {
		return nil, ErrSequenceExhausted
	}
	var nonce [chacha20poly1305.NonceSize]byte
	binary.LittleEndian.PutUint64(nonce[4:12], c.n)
	ct := c.aead.Seal(nil, nonce[:], plaintext, ad)
	c.n++
	return ct, nil
}

func (c *noiseCipherState) decryptWithAd(ad, ciphertext []byte) ([]byte, error) {
	if !c.hasKey {
		return slices.Clone(ciphertext), nil
	}
	if c.n == math.MaxUint64 {
		return nil, ErrSequenceExhausted
	}
	var nonce [chacha20poly1305.NonceSize]byte
	binary.LittleEndian.PutUint64(nonce[4:12], c.n)
	pt, err := c.aead.Open(nil, nonce[:], ciphertext, ad)
	if err != nil {
		return nil, errNoiseAuth
	}
	c.n++
	return pt, nil
}

// noiseSymmetricState implements Noise Protocol Revision 34 §5.2 for ChaChaPoly + SHA256.
type noiseSymmetricState struct {
	cs noiseCipherState
	ck [sha256.Size]byte
	h  [sha256.Size]byte
}

func (s *noiseSymmetricState) init(protocolName string) {
	s.h = [sha256.Size]byte{}
	if len(protocolName) <= sha256.Size {
		copy(s.h[:], protocolName)
	} else {
		s.h = sha256.Sum256([]byte(protocolName))
	}
	s.ck = s.h
	s.cs = noiseCipherState{}
}

func (s *noiseSymmetricState) mixHash(data []byte) {
	h := sha256.New()
	h.Write(s.h[:])
	h.Write(data)
	h.Sum(s.h[:0])
}

func (s *noiseSymmetricState) mixKey(ikm []byte) {
	ck, tempK := noiseHKDF2(s.ck, ikm)
	s.ck = ck
	s.cs.initKey(tempK)
	clear(tempK[:])
}

func (s *noiseSymmetricState) encryptAndHash(plaintext []byte) ([]byte, error) {
	ct, err := s.cs.encryptWithAd(s.h[:], plaintext)
	if err != nil {
		return nil, err
	}
	s.mixHash(ct)
	return ct, nil
}

func (s *noiseSymmetricState) decryptAndHash(ciphertext []byte) ([]byte, error) {
	pt, err := s.cs.decryptWithAd(s.h[:], ciphertext)
	if err != nil {
		return nil, err
	}
	s.mixHash(ciphertext)
	return pt, nil
}

func (s *noiseSymmetricState) split() (k1, k2 [32]byte) {
	k1, k2 = noiseHKDF2(s.ck, nil)
	clear(s.ck[:])
	return k1, k2
}

// noiseHKDF2 implements Noise Protocol Revision 34 §4.3 HKDF with 2 outputs over HMAC-SHA256.
func noiseHKDF2(ck [32]byte, ikm []byte) (out1, out2 [32]byte) {
	var tempKey [sha256.Size]byte
	mac := hmac.New(sha256.New, ck[:])
	mac.Write(ikm)
	mac.Sum(tempKey[:0])

	mac1 := hmac.New(sha256.New, tempKey[:])
	mac1.Write([]byte{0x01})
	mac1.Sum(out1[:0])

	mac2 := hmac.New(sha256.New, tempKey[:])
	mac2.Write(out1[:])
	mac2.Write([]byte{0x02})
	mac2.Sum(out2[:0])

	clear(tempKey[:])
	return out1, out2
}

// noiseNKInitiator runs the initiator side of Noise_NK_25519_ChaChaPoly_SHA256 (<- s ... -> e, es <- e, ee).
type noiseNKInitiator struct {
	ss noiseSymmetricState
	e  *ecdh.PrivateKey
	rs *ecdh.PublicKey
}

func newNoiseNKInitiator(prologue []byte, responderStatic *ecdh.PublicKey, ephemeral *ecdh.PrivateKey) (*noiseNKInitiator, error) {
	if err := validateX25519PublicKey(responderStatic); err != nil {
		return nil, err
	}
	if ephemeral == nil {
		var err error
		ephemeral, err = ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
	} else if err := validateX25519PrivateKey(ephemeral); err != nil {
		return nil, err
	}
	i := &noiseNKInitiator{e: ephemeral, rs: responderStatic}
	i.ss.init(noiseNKProtocolName)
	i.ss.mixHash(prologue)
	i.ss.mixHash(responderStatic.Bytes())
	return i, nil
}

func (i *noiseNKInitiator) writeMessage1(payload []byte) ([]byte, error) {
	ePub := i.e.PublicKey().Bytes()
	i.ss.mixHash(ePub)
	es, err := i.e.ECDH(i.rs)
	if err != nil {
		return nil, err
	}
	i.ss.mixKey(es)
	clear(es)
	ct, err := i.ss.encryptAndHash(payload)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(ePub)+len(ct))
	copy(out, ePub)
	copy(out[len(ePub):], ct)
	return out, nil
}

// readMessage2 verifies and decrypts message 2 on a copy of the symmetric state so a forged or corrupted datagram cannot poison the handshake while waiting for the genuine reply.
func (i *noiseNKInitiator) readMessage2(msg []byte) (payload []byte, c2sKey, s2cKey [32]byte, err error) {
	if len(msg) < udpNoiseMsgOverhead {
		return nil, c2sKey, s2cKey, ErrInvalidPacket
	}
	re, err := ecdh.X25519().NewPublicKey(msg[:32])
	if err != nil {
		return nil, c2sKey, s2cKey, ErrInvalidPacket
	}
	ss := i.ss
	ss.mixHash(msg[:32])
	ee, err := i.e.ECDH(re)
	if err != nil {
		return nil, c2sKey, s2cKey, errNoiseAuth
	}
	ss.mixKey(ee)
	clear(ee)
	pt, err := ss.decryptAndHash(msg[32:])
	if err != nil {
		return nil, c2sKey, s2cKey, err
	}
	c2sKey, s2cKey = ss.split()
	i.ss = ss
	return pt, c2sKey, s2cKey, nil
}

// noiseNKResponder runs the responder side of Noise_NK_25519_ChaChaPoly_SHA256.
type noiseNKResponder struct {
	ss noiseSymmetricState
	s  *ecdh.PrivateKey
	re *ecdh.PublicKey
}

func newNoiseNKResponder(prologue []byte, staticKey *ecdh.PrivateKey) (*noiseNKResponder, error) {
	if err := validateX25519PrivateKey(staticKey); err != nil {
		return nil, err
	}
	r := &noiseNKResponder{s: staticKey}
	r.ss.init(noiseNKProtocolName)
	r.ss.mixHash(prologue)
	r.ss.mixHash(staticKey.PublicKey().Bytes())
	return r, nil
}

func (r *noiseNKResponder) readMessage1(msg []byte) ([]byte, error) {
	if len(msg) < udpNoiseMsgOverhead {
		return nil, ErrInvalidPacket
	}
	re, err := ecdh.X25519().NewPublicKey(msg[:32])
	if err != nil {
		return nil, ErrInvalidPacket
	}
	r.ss.mixHash(msg[:32])
	es, err := r.s.ECDH(re)
	if err != nil {
		return nil, errNoiseAuth
	}
	r.ss.mixKey(es)
	clear(es)
	pt, err := r.ss.decryptAndHash(msg[32:])
	if err != nil {
		return nil, err
	}
	r.re = re
	return pt, nil
}

func (r *noiseNKResponder) writeMessage2(payload []byte, ephemeral *ecdh.PrivateKey) (msg []byte, c2sKey, s2cKey [32]byte, err error) {
	if ephemeral == nil {
		ephemeral, err = ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return nil, c2sKey, s2cKey, err
		}
	} else if err := validateX25519PrivateKey(ephemeral); err != nil {
		return nil, c2sKey, s2cKey, err
	}
	ePub := ephemeral.PublicKey().Bytes()
	r.ss.mixHash(ePub)
	ee, err := ephemeral.ECDH(r.re)
	if err != nil {
		return nil, c2sKey, s2cKey, err
	}
	r.ss.mixKey(ee)
	clear(ee)
	ct, err := r.ss.encryptAndHash(payload)
	if err != nil {
		return nil, c2sKey, s2cKey, err
	}
	c2sKey, s2cKey = r.ss.split()
	out := make([]byte, len(ePub)+len(ct))
	copy(out, ePub)
	copy(out[len(ePub):], ct)
	return out, c2sKey, s2cKey, nil
}

// encodeUDPHelloV2 builds a padded UDP handshake version 2 hello encrypted with Noise NK.
func encodeUDPHelloV2(serverKey *ecdh.PublicKey, h hello) ([]byte, *noiseNKInitiator, error) {
	if len(h.token) > MaxTokenSize {
		return nil, nil, ErrTokenTooLarge
	}
	var prefix [udpHelloPrefixSize]byte
	prefix[0] = hsHello
	prefix[1] = udpHandshakeVersion
	copy(prefix[2:udpHelloPrefixSize], h.nonce[:])

	init, err := newNoiseNKInitiator(prefix[1:udpHelloPrefixSize], serverKey, nil)
	if err != nil {
		return nil, nil, err
	}
	plainLen := max(udpHelloBodyHeader+len(h.token), udpMinHelloPlain)
	plain := make([]byte, plainLen)
	binary.BigEndian.PutUint32(plain[0:4], uint32(h.resumePeer))
	copy(plain[4:20], h.resumeSecret[:])
	binary.BigEndian.PutUint16(plain[20:22], uint16(len(h.token)))
	copy(plain[22:], h.token)

	msg1, err := init.writeMessage1(plain)
	clear(plain)
	if err != nil {
		return nil, nil, err
	}
	dgram := make([]byte, udpHelloPrefixSize+len(msg1))
	copy(dgram[:udpHelloPrefixSize], prefix[:])
	copy(dgram[udpHelloPrefixSize:], msg1)
	return dgram, init, nil
}

// decodeUDPHelloV2 decrypts a UDP handshake version 2 hello. A hello of another version returns its nonce with errHandshakeVersion.
func decodeUDPHelloV2(staticKey *ecdh.PrivateKey, b []byte) (*noiseNKResponder, hello, error) {
	var h hello
	if len(b) < 2 || b[0] != hsHello {
		return nil, h, ErrInvalidPacket
	}
	if len(b) >= udpHelloPrefixSize {
		copy(h.nonce[:], b[2:udpHelloPrefixSize])
	}
	if b[1] != udpHandshakeVersion {
		return nil, h, errHandshakeVersion
	}
	if len(b) < udpHelloPrefixSize+udpNoiseMsgOverhead+udpHelloBodyHeader || len(b) > udpMaxHello {
		return nil, h, ErrInvalidPacket
	}
	resp, err := newNoiseNKResponder(b[1:udpHelloPrefixSize], staticKey)
	if err != nil {
		return nil, h, err
	}
	plain, err := resp.readMessage1(b[udpHelloPrefixSize:])
	if err != nil {
		return nil, h, err
	}
	if len(plain) < udpHelloBodyHeader {
		return nil, h, ErrInvalidPacket
	}
	h.resumePeer = PeerID(binary.BigEndian.Uint32(plain[0:4]))
	copy(h.resumeSecret[:], plain[4:20])
	tokenLen := int(binary.BigEndian.Uint16(plain[20:22]))
	if tokenLen > MaxTokenSize || udpHelloBodyHeader+tokenLen > len(plain) {
		return nil, h, ErrInvalidPacket
	}
	h.token = slices.Clone(plain[udpHelloBodyHeader : udpHelloBodyHeader+tokenLen])
	return resp, h, nil
}

// encodeUDPWelcomeV2 encrypts w as Noise NK message 2 and returns the datagram and the directional AEAD keys from Split.
func encodeUDPWelcomeV2(resp *noiseNKResponder, w welcome) (dgram []byte, c2sKey, s2cKey [32]byte, err error) {
	var body [udpWelcomeBodySize]byte
	body[0] = hsWelcome
	binary.BigEndian.PutUint32(body[1:5], uint32(w.peer))
	binary.BigEndian.PutUint32(body[5:9], uint32(w.server))
	if w.resumed {
		body[9] = 1
	}
	copy(body[10:26], w.secret[:])

	msg2, c2sKey, s2cKey, err := resp.writeMessage2(body[:], nil)
	clear(body[:])
	if err != nil {
		return nil, c2sKey, s2cKey, err
	}
	dgram = make([]byte, udpHelloPrefixSize+len(msg2))
	dgram[0] = hsWelcome
	dgram[1] = udpHandshakeVersion
	copy(dgram[2:udpHelloPrefixSize], w.nonce[:])
	copy(dgram[udpHelloPrefixSize:], msg2)
	return dgram, c2sKey, s2cKey, nil
}

// encodeUDPRejectV2 encrypts a rejection as Noise NK message 2, truncating reason so the datagram never exceeds limit bytes when limit > 0.
func encodeUDPRejectV2(resp *noiseNKResponder, nonce [hsNonceSize]byte, reason string, limit int) ([]byte, error) {
	if len(reason) > hsMaxReason {
		reason = reason[:hsMaxReason]
	}
	if limit > 0 && udpRejectOverhead+len(reason) > limit {
		reason = reason[:max(0, limit-udpRejectOverhead)]
	}
	body := make([]byte, udpRejectBodyHeader+len(reason))
	body[0] = hsReject
	binary.BigEndian.PutUint16(body[1:3], uint16(len(reason)))
	copy(body[3:], reason)

	msg2, c2sKey, s2cKey, err := resp.writeMessage2(body, nil)
	clear(c2sKey[:])
	clear(s2cKey[:])
	if err != nil {
		return nil, err
	}
	dgram := make([]byte, udpHelloPrefixSize+len(msg2))
	dgram[0] = hsReject
	dgram[1] = udpHandshakeVersion
	copy(dgram[2:udpHelloPrefixSize], nonce[:])
	copy(dgram[udpHelloPrefixSize:], msg2)
	return dgram, nil
}

// acceptUDPServerHandshakeV2 parses and authenticates a version 2 welcome or reject. A version-1 cleartext version rejection (such as from an older server) is also recognized as a *RejectError.
func acceptUDPServerHandshakeV2(init *noiseNKInitiator, nonce [hsNonceSize]byte, b []byte) (w welcome, c2sKey, s2cKey [32]byte, err error) {
	if len(b) < udpHelloPrefixSize {
		return w, c2sKey, s2cKey, ErrInvalidPacket
	}
	if b[0] == hsReject && b[1] == hsVersion {
		w1, err1 := acceptServerHandshake(b, nonce)
		if err1 != nil && !errors.Is(err1, ErrInvalidPacket) && w1.nonce == nonce {
			return w1, c2sKey, s2cKey, err1
		}
		return w, c2sKey, s2cKey, ErrInvalidPacket
	}
	if b[1] != udpHandshakeVersion || (b[0] != hsWelcome && b[0] != hsReject) {
		return w, c2sKey, s2cKey, ErrInvalidPacket
	}
	var replyNonce [hsNonceSize]byte
	copy(replyNonce[:], b[2:udpHelloPrefixSize])
	if replyNonce != nonce {
		return w, c2sKey, s2cKey, ErrInvalidPacket
	}
	initCopy := *init
	plain, c2s, s2c, err := initCopy.readMessage2(b[udpHelloPrefixSize:])
	if err != nil {
		return w, c2sKey, s2cKey, ErrInvalidPacket
	}
	if len(plain) < 1 || plain[0] != b[0] {
		return w, c2sKey, s2cKey, ErrInvalidPacket
	}
	switch b[0] {
	case hsWelcome:
		if len(plain) != udpWelcomeBodySize || plain[9] > 1 {
			return w, c2sKey, s2cKey, ErrInvalidPacket
		}
		w.nonce = replyNonce
		w.peer = PeerID(binary.BigEndian.Uint32(plain[1:5]))
		w.server = PeerID(binary.BigEndian.Uint32(plain[5:9]))
		if w.peer == 0 || w.server == 0 || w.peer == w.server {
			return w, c2sKey, s2cKey, ErrInvalidPacket
		}
		w.resumed = plain[9] == 1
		copy(w.secret[:], plain[10:26])
		*init = initCopy
		return w, c2s, s2c, nil
	case hsReject:
		clear(c2s[:])
		clear(s2c[:])
		if len(plain) < udpRejectBodyHeader {
			return w, c2sKey, s2cKey, ErrInvalidPacket
		}
		n := int(binary.BigEndian.Uint16(plain[1:3]))
		if n > hsMaxReason || udpRejectBodyHeader+n != len(plain) {
			return w, c2sKey, s2cKey, ErrInvalidPacket
		}
		w.nonce = replyNonce
		*init = initCopy
		return w, c2sKey, s2cKey, &RejectError{Reason: string(plain[3:])}
	default:
		return w, c2sKey, s2cKey, ErrInvalidPacket
	}
}

// udpCipher seals and opens UDP datagrams with ChaCha20-Poly1305. Keeping the 12-byte nonce array inside the struct avoids escaping a stack nonce across the cipher.AEAD interface on every packet, keeping both hot paths allocation-free.
type udpCipher struct {
	aead  cipher.AEAD
	nonce [chacha20poly1305.NonceSize]byte
}

func newUDPCipher(key [chacha20poly1305.KeySize]byte) udpCipher {
	aead, err := chacha20poly1305.New(key[:])
	if err != nil {
		panic(err) // key is always 32 bytes
	}
	return udpCipher{aead: aead}
}

// seal writes the 10-byte cleartext header followed by ciphertext and 16-byte Poly1305 tag into dst[:0]. When cap(dst) >= udpSealedHeader+len(payload), seal performs zero heap allocations.
func (c *udpCipher) seal(dst []byte, dir uint32, typ byte, peer PeerID, seq uint32, ch Channel, payload []byte) []byte {
	need := udpSealedHeader + len(payload)
	if cap(dst) < need {
		dst = make([]byte, udpCleartextHeader, need)
	} else {
		dst = dst[:udpCleartextHeader]
	}
	dst[0] = typ
	binary.BigEndian.PutUint32(dst[1:5], uint32(peer))
	binary.BigEndian.PutUint32(dst[5:9], seq)
	dst[9] = byte(ch)
	binary.BigEndian.PutUint32(c.nonce[0:4], dir)
	binary.BigEndian.PutUint32(c.nonce[4:8], uint32(peer))
	binary.BigEndian.PutUint32(c.nonce[8:12], seq)
	return c.aead.Seal(dst, c.nonce[:], payload, dst)
}

// open authenticates and decrypts b into dst[:0]. When cap(dst) >= len(b)-udpSealedHeader, open performs zero heap allocations.
func (c *udpCipher) open(dst []byte, dir uint32, b []byte) ([]byte, bool) {
	if c.aead == nil || len(b) < udpSealedHeader {
		return nil, false
	}
	peer := binary.BigEndian.Uint32(b[1:5])
	seq := binary.BigEndian.Uint32(b[5:9])
	binary.BigEndian.PutUint32(c.nonce[0:4], dir)
	binary.BigEndian.PutUint32(c.nonce[4:8], peer)
	binary.BigEndian.PutUint32(c.nonce[8:12], seq)
	pt, err := c.aead.Open(dst[:0], c.nonce[:], b[udpCleartextHeader:], b[:udpCleartextHeader])
	if err != nil {
		return nil, false
	}
	return pt, true
}

// sealedHeader is a parsed, not yet authenticated, sealed datagram header.
type sealedHeader struct {
	typ  byte
	peer PeerID
	seq  uint32
	ch   Channel
}

func parseSealed(b []byte) (sealedHeader, bool) {
	if len(b) < udpSealedHeader {
		return sealedHeader{}, false
	}
	return sealedHeader{
		typ:  b[0],
		peer: PeerID(binary.BigEndian.Uint32(b[1:5])),
		seq:  binary.BigEndian.Uint32(b[5:9]),
		ch:   Channel(b[9]),
	}, true
}

// nextSeq advances seq without wrapping past math.MaxUint32, preventing AEAD nonce reuse.
func nextSeq(seq *uint32) (uint32, bool) {
	if *seq == math.MaxUint32 {
		return 0, false
	}
	*seq++
	return *seq, true
}

// replayWindow refuses a sequence number that is zero, seen before, or older than the last 64, so a captured datagram cannot be replayed and a wrapped sequence number is rejected once top reaches math.MaxUint32.
type replayWindow struct {
	top  uint32
	seen uint64
}

// wouldAccept reports whether seq is currently acceptable without mutating the window, allowing callers to reject obvious replays before spending AEAD work.
func (w *replayWindow) wouldAccept(seq uint32) bool {
	switch {
	case seq == 0:
		return false
	case seq > w.top:
		return true
	case w.top-seq >= 64:
		return false
	default:
		bit := uint64(1) << (w.top - seq)
		return w.seen&bit == 0
	}
}

func (w *replayWindow) accept(seq uint32) bool {
	switch {
	case seq == 0:
		return false
	case seq > w.top:
		shift := seq - w.top
		if shift >= 64 {
			w.seen = 0
		} else {
			w.seen <<= shift
		}
		w.seen |= 1
		w.top = seq
		return true
	case w.top-seq >= 64:
		return false
	default:
		bit := uint64(1) << (w.top - seq)
		if w.seen&bit != 0 {
			return false
		}
		w.seen |= bit
		return true
	}
}
