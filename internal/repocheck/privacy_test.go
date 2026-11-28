package repocheck

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestPrivacyExemptionsRoundTripAndUnknownFields(t *testing.T) {
	raw := []byte("version: 1\nentries:\n  - path: docs/example.md\n    reason: synthetic fixture\n")
	x, err := ParsePrivacyExemptions(raw)
	if err != nil || len(x.Entries) != 1 {
		t.Fatal(x, err)
	}
	if _, err := ParsePrivacyExemptions([]byte("version: 1\nentries: [{path: x, why: bad}]\n")); err == nil {
		t.Fatal("unknown field accepted")
	}
}
func TestNoPrivatePaths(t *testing.T) {
	good := fstest.MapFS{"docs/good.md": {Data: []byte("/Users/you/games/example")}, "docs/bad.md": {Data: []byte("/Users/alice/games/private")}}
	got, err := ScanPrivacy(good, PrivacyExemptions{Version: 1})
	if err != nil || len(got) != 1 || got[0].Reason != "personal path" {
		t.Fatalf("findings=%+v err=%v", got, err)
	}
}
func TestNoPrivateFiles(t *testing.T) {
	f := fstest.MapFS{".env": {Data: []byte("TOKEN=not-for-public")}, "ok.txt": {Data: []byte("safe")}}
	got, err := ScanPrivacy(f, PrivacyExemptions{Version: 1})
	if err != nil || len(got) != 1 || got[0].Path != ".env" {
		t.Fatalf("findings=%+v err=%v", got, err)
	}
	if !fs.ValidPath(got[0].Path) {
		t.Fatal("bad path")
	}
}
