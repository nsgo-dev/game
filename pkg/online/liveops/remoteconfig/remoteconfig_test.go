package remoteconfig

import "testing"

func TestStableBucketAndActivate(t *testing.T) {
	r := New(Config{Values: map[string]Value{"flag": {Type: Bool, Default: false, Overrides: []Override{{Salt: "s", Percent: 10000, Variant: true}}}}})
	if !r.GetBool("flag", "acct") {
		t.Fatal("override")
	}
	r.Stage(Config{Values: map[string]Value{"flag": {Type: Bool, Default: false}}})
	if !r.GetBool("flag", "acct") {
		t.Fatal("stage leaked")
	}
	r.Activate()
	if r.GetBool("flag", "acct") {
		t.Fatal("activate")
	}
	if Bucket("s", "acct") != Bucket("s", "acct") {
		t.Fatal("unstable bucket")
	}
}
