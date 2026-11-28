package crashreport

import "testing"

func TestFingerprintIgnoresLineAndGoroutineDetails(t *testing.T) {
	a := CrashReport{Message: "boom", Stack: "goroutine 1 [running]:\nmain.run()\n\t/tmp/game.go:10 +0x1\nruntime.goexit()\n\t/usr/local/go/src/runtime/asm_amd64.s:1700 +0x1\n"}
	b := a
	b.Stack = "goroutine 99 [running]:\nmain.run()\n\t/tmp/game.go:99 +0x8\nruntime.goexit()\n\t/usr/local/go/src/runtime/asm_amd64.s:1800 +0x1\n"
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("fingerprint changed with line or goroutine details")
	}
}
