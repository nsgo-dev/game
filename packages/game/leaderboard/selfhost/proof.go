package selfhost

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/accounts/token"
)

// proofPrefix starts every proof, naming its format.
const proofPrefix = "v1"

// proofDomain separates the bytes a proof signs from anything else the same key might sign.
const proofDomain = "nsgo-leaderboard-proof-v1"

var proofB64 = base64.RawURLEncoding.Strict()

// ProofSigner issues score proofs on a game server: after the server has decided a match or checked a replay, it signs the board, the account and the score, and the player's client submits the proof with the score. A board with validation: signed accepts a score only with such a proof, made within the configuration's proofTTL by a key listed in its serverKeys.
//
// A proof is the text "v1.<key id>.<unix seconds>.<signature>", the signature an Ed25519 signature in unpadded base64url. It holds no secret, but it lets anyone holding it submit that score for that account, so the server hands it only to that player.
type ProofSigner struct {
	key   ed25519.PrivateKey
	keyID string
	// Now is the signer's clock; nil uses time.Now.
	Now func() time.Time
}

// NewProofSigner returns a signer for key, such as one read with token.ParsePrivateKey from the PEM file leaderboardd genkey writes.
func NewProofSigner(key ed25519.PrivateKey) (*ProofSigner, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("selfhost: not an Ed25519 private key")
	}
	return &ProofSigner{key: key, keyID: token.KeyID(key.Public().(ed25519.PublicKey))}, nil
}

// PublicKey returns the signer's public key as a serverKeys entry for the leaderboard configuration.
func (s *ProofSigner) PublicKey() token.PublicKey {
	pub := s.key.Public().(ed25519.PublicKey)
	return token.PublicKey{ID: s.keyID, Key: base64.StdEncoding.EncodeToString(pub)}
}

// Sign returns a proof that the account scored score on the board now.
func (s *ProofSigner) Sign(board, account string, score int64) (string, error) {
	if !validName(board) {
		return "", fmt.Errorf("selfhost: invalid board name %q", board)
	}
	if !validAccount(account) {
		return "", ErrInvalidAccount
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	issued := now().Unix()
	sig := ed25519.Sign(s.key, proofMessage(board, account, score, issued))
	return proofPrefix + "." + s.keyID + "." + strconv.FormatInt(issued, 10) + "." + proofB64.EncodeToString(sig), nil
}

// proofMessage is the bytes a proof signs. Board names and account IDs hold no NUL, so the fields cannot run into each other.
func proofMessage(board, account string, score, issued int64) []byte {
	msg := make([]byte, 0, len(proofDomain)+len(board)+len(account)+48)
	msg = append(msg, proofDomain...)
	msg = append(msg, 0)
	msg = append(msg, board...)
	msg = append(msg, 0)
	msg = append(msg, account...)
	msg = append(msg, 0)
	msg = strconv.AppendInt(msg, score, 10)
	msg = append(msg, 0)
	msg = strconv.AppendInt(msg, issued, 10)
	return msg
}

// verifyProof checks that proof is a well-formed proof of the score signed by one of keys, and returns when it was issued.
func verifyProof(keys map[string]ed25519.PublicKey, proof, board, account string, score int64) (time.Time, error) {
	// The base64 decoder skips line breaks, so refuse every byte outside the proof alphabet first: one signed score has exactly one proof text.
	for i := 0; i < len(proof); i++ {
		if c := proof[i]; !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
			return time.Time{}, ErrInvalidProof
		}
	}
	parts := strings.Split(proof, ".")
	if len(parts) != 4 || parts[0] != proofPrefix || !validName(board) || !validAccount(account) {
		return time.Time{}, ErrInvalidProof
	}
	pub, ok := keys[parts[1]]
	if !ok {
		return time.Time{}, ErrInvalidProof
	}
	issued, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || strconv.FormatInt(issued, 10) != parts[2] {
		return time.Time{}, ErrInvalidProof
	}
	sig, err := proofB64.DecodeString(parts[3])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return time.Time{}, ErrInvalidProof
	}
	if !ed25519.Verify(pub, proofMessage(board, account, score, issued), sig) {
		return time.Time{}, ErrInvalidProof
	}
	return time.Unix(issued, 0).UTC(), nil
}
