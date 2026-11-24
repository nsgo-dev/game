package resource

import (
	"crypto/rand"
	"fmt"
	"io"
)

// UIDLength is how many characters a UID has.
const UIDLength = 13

// uidAlphabet is Crockford's base 32 in lower case: digits and letters without i, l, o and u, so a UID reads aloud and copies by hand without ambiguity.
const uidAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// NewUID returns a new UID, UIDLength characters of uidAlphabet that carry 65 random bits read from r, or from crypto/rand when r is nil. A UID is the stable ID a scene or resource file keeps in its header (SceneDoc.UID in sceneformat, Header.UID here), which stays the same when the file is renamed or moved, so the tools can find a moved file again (see package refs).
func NewUID(r io.Reader) (string, error) {
	if r == nil {
		r = rand.Reader
	}
	var b [9]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", fmt.Errorf("resource: new uid: %w", err)
	}
	var acc uint64
	bits := 0
	out := make([]byte, 0, UIDLength)
	for _, c := range b {
		acc = acc<<8 | uint64(c)
		bits += 8
		for bits >= 5 && len(out) < UIDLength {
			bits -= 5
			out = append(out, uidAlphabet[(acc>>bits)&31])
		}
	}
	return string(out), nil
}

// ValidUID reports whether s has the form NewUID returns: UIDLength characters, each a digit or a lower-case letter other than i, l, o and u.
func ValidUID(s string) bool {
	if len(s) != UIDLength {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z' || c == 'i' || c == 'l' || c == 'o' || c == 'u') {
			return false
		}
	}
	return true
}
