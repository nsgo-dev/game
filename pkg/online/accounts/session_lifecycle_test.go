package accounts

import (
	"errors"
	"testing"
	"time"
)

func TestSessionDirectoryExpiryRotationAndReuse(t *testing.T) {
	now := time.Unix(100, 0)
	d := NewSessionDirectory(SessionPolicy{IdleTTL: time.Minute, Rotate: true, Overlap: time.Second}, func() time.Time { return now })
	d.Open(DeviceSession{ID: "s", AccountID: "a", Device: "phone", Created: now}, "old")
	s, e := d.Refresh("s", "old", "new")
	if e != nil || s.Generation != 2 {
		t.Fatalf("refresh=%+v %v", s, e)
	}
	if _, e = d.Check("s", "old"); e != nil {
		t.Fatal("overlap failed")
	}
	now = now.Add(2 * time.Second)
	if _, e = d.Check("s", "old"); !errors.Is(e, ErrSessionReuse) {
		t.Fatalf("reuse=%v", e)
	}
	d.Open(DeviceSession{ID: "s2", AccountID: "a", Created: now}, "x")
	now = now.Add(time.Minute)
	if d.Sweep() != 1 {
		t.Fatal("sweep failed")
	}
}
