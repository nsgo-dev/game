package postfx

import "testing"

func TestFlashLimiterCapsRollingWindow(t *testing.T) {
	l := &FlashLimiter{MaxPerSecond: 3}
	if !l.Allow() || !l.Allow() || !l.Allow() {
		t.Fatal("first three flashes should be allowed")
	}
	if l.Allow() {
		t.Fatal("fourth flash should be rate limited")
	}
}
