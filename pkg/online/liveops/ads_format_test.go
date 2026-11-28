package liveops

import (
	"strings"
	"testing"
	"time"
)

func TestAdsConfigRoundTripAndValidation(t *testing.T) {
	raw := []byte("version: 1\nplatforms:\n  wechat:\n    rewarded:\n      coin: adunit-reward\n    interstitialInterval: 1m0s\n    interstitialCap: 3\n")
	c, err := ParseAdsConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.Platforms["wechat"].InterstitialInterval != time.Minute {
		t.Fatal("interval")
	}
	out, err := EncodeAdsConfig(c)
	if err != nil || !strings.Contains(string(out), "adunit-reward") {
		t.Fatalf("out=%s err=%v", out, err)
	}
	if _, err := ParseAdsConfig([]byte("version: 1\nplatforms:\n  wechat:\n    unknown: x\n")); err == nil {
		t.Fatal("unknown field accepted")
	}
}
