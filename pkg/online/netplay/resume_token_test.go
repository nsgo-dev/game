package netplay

import "testing"

func TestResumeTokenRoundTrip(t *testing.T) {
	r := resumeState{peer: 42}
	for i := range r.secret {
		r.secret[i] = byte(i)
	}
	back, err := decodeResumeToken(encodeResumeToken(r))
	if err != nil || back != r {
		t.Fatalf("back=%+v err=%v", back, err)
	}
}
