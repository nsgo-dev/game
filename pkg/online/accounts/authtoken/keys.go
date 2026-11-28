package authtoken

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// KeySetVersion is the key set file schema version this package reads and writes.
const KeySetVersion = 1

// KeySet is the public half of an account service's signing keys, kept in a YAML file a game server loads to verify tokens offline:
//
//	version: 1
//	issuer: https://accounts.example.com
//	audience: my-game
//	keys:
//	  - id: 2317febd373fcbcd
//	    key: hze+0cYruVRddgjkbHzgCeWWJbEsBkn6K8JNHRDf6Wg=
//
// Several keys let a service rotate its signing key: publish the new key beside the old one, switch the service to it, and drop the old key once its last tokens have expired.
type KeySet struct {
	// Version is the schema version; it must be KeySetVersion.
	Version int `yaml:"version"`
	// Issuer is the issuer the verifier accepts.
	Issuer string `yaml:"issuer"`
	// Audience is the audience the verifier accepts.
	Audience string `yaml:"audience"`
	// Keys are the public keys tokens may be signed with.
	Keys []PublicKey `yaml:"keys"`
}

// PublicKey is one Ed25519 public key of a key set.
type PublicKey struct {
	// ID is KeyID of the key; it must match the key.
	ID string `yaml:"id"`
	// Key is the 32-byte public key in standard base64.
	Key string `yaml:"key"`
}

func (k PublicKey) publicKey() (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.Strict().DecodeString(k.Key)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("authtoken: key %q is not a base64 Ed25519 public key", k.ID)
	}
	return ed25519.PublicKey(raw), nil
}

// Validate checks that ks is a version 1 key set with an issuer, an audience and at least one well-formed key, each under its own KeyID once.
func (ks *KeySet) Validate() error {
	if ks == nil {
		return loaderr.Errorf("", "pass a non-nil *authtoken.KeySet to Validate", "authtoken: nil key set")
	}
	if ks.Version != KeySetVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the key set", KeySetVersion), "authtoken: unsupported key set version %d (want %d)", ks.Version, KeySetVersion)
	}
	if ks.Issuer == "" {
		return loaderr.Errorf("issuer", "set issuer to the account service identifier such as its URL", "authtoken: key set needs an issuer and an audience")
	}
	if ks.Audience == "" {
		return loaderr.Errorf("audience", "set audience to the game identifier the tokens are issued for", "authtoken: key set needs an issuer and an audience")
	}
	if len(ks.Keys) == 0 {
		return loaderr.Errorf("keys", "add at least one Ed25519 public key entry under keys", "authtoken: key set has no keys")
	}
	seen := make(map[string]bool, len(ks.Keys))
	for i, k := range ks.Keys {
		kPath := fmt.Sprintf("keys[%d]", i)
		pub, err := k.publicKey()
		if err != nil {
			return loaderr.Errorf(kPath+".key", "set key to a 32-byte Ed25519 public key encoded in standard base64", "%s", err.Error())
		}
		if id := KeyID(pub); k.ID != id {
			return loaderr.Errorf(kPath+".id", fmt.Sprintf("set id to %q to match the public key", id), "authtoken: key id %q does not match its key (want %q)", k.ID, id)
		}
		if seen[k.ID] {
			return loaderr.Errorf(kPath+".id", "remove the duplicate public key entry", "authtoken: key %q listed twice", k.ID)
		}
		seen[k.ID] = true
	}
	return nil
}

// ParseKeySet decodes and validates a YAML key set. Unknown fields and trailing documents are refused.
func ParseKeySet(data []byte) (*KeySet, error) {
	var ks KeySet
	if err := decodeYAML(data, &ks); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("authtoken: key set: %w", err), "", data, "check the YAML syntax and field names in the key set")
	}
	if err := ks.Validate(); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	return &ks, nil
}

// Marshal validates ks and encodes it as deterministic 2-space YAML, keys in their order.
func (ks *KeySet) Marshal() ([]byte, error) {
	if err := ks.Validate(); err != nil {
		return nil, err
	}
	return encodeYAML(ks)
}

// Merge returns a key set with the keys of ks followed by those of other it lacks. Both must have the same issuer and audience.
func (ks *KeySet) Merge(other *KeySet) (*KeySet, error) {
	if err := ks.Validate(); err != nil {
		return nil, err
	}
	if err := other.Validate(); err != nil {
		return nil, err
	}
	if ks.Issuer != other.Issuer || ks.Audience != other.Audience {
		return nil, errors.New("authtoken: key sets of different issuers or audiences")
	}
	out := &KeySet{Version: KeySetVersion, Issuer: ks.Issuer, Audience: ks.Audience, Keys: append([]PublicKey(nil), ks.Keys...)}
	have := make(map[string]bool, len(ks.Keys))
	for _, k := range ks.Keys {
		have[k.ID] = true
	}
	for _, k := range other.Keys {
		if !have[k.ID] {
			out.Keys = append(out.Keys, k)
		}
	}
	return out, nil
}

// GenerateKey returns a new Ed25519 signing key read from r, or crypto/rand when r is nil.
func GenerateKey(r io.Reader) (ed25519.PrivateKey, error) {
	if r == nil {
		r = rand.Reader
	}
	_, priv, err := ed25519.GenerateKey(r)
	if err != nil {
		return nil, fmt.Errorf("authtoken: generate key: %w", err)
	}
	return priv, nil
}

// MarshalPrivateKey encodes key as a PKCS #8 "PRIVATE KEY" PEM block, the text form the account service keeps its signing key in.
func MarshalPrivateKey(key ed25519.PrivateKey) ([]byte, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("authtoken: not an Ed25519 private key")
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("authtoken: marshal key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// ParsePrivateKey decodes a PKCS #8 PEM block holding an Ed25519 private key. Errors never include the key material.
func ParsePrivateKey(data []byte) (ed25519.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("authtoken: want a PEM \"PRIVATE KEY\" block")
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("authtoken: data after the PEM block")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("authtoken: malformed PKCS #8 private key")
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("authtoken: the private key is not Ed25519")
	}
	return priv, nil
}

// decodeYAML decodes one YAML document into v, refusing unknown fields and further documents.
func decodeYAML(data []byte, v any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return errors.New("empty document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing YAML document")
		}
		return err
	}
	return nil
}

// encodeYAML encodes v as 2-space YAML.
func encodeYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
