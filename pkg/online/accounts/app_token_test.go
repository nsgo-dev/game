package accounts

import (
	"strings"
	"testing"
)

func TestAppAccountToken(t *testing.T) {
	a := AppAccountToken("abcdef0123456789")
	if len(a) != 36 || strings.Count(a, "-") != 4 || a != AppAccountToken("abcdef0123456789") {
		t.Fatalf("token=%q", a)
	}
}
