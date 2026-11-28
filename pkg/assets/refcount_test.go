package assets

import "testing"

func TestRefCacheSharesAndClosesAtLastRelease(t *testing.T) {
	c := NewRefCache[string]()
	loads, closes := 0, 0
	a, _ := c.Acquire("a", func() (string, error) { loads++; return "x", nil }, func(string) { closes++ })
	b, _ := c.Acquire("a", func() (string, error) { loads++; return "y", nil }, func(string) { closes++ })
	if a.Value() != b.Value() || loads != 1 {
		t.Fatalf("value=%q/%q loads=%d", a.Value(), b.Value(), loads)
	}
	a.Release()
	if closes != 0 {
		t.Fatal("closed too early")
	}
	b.Release()
	if closes != 1 {
		t.Fatal("not closed")
	}
}
