package liveops_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/liveops"
)

func TestCodeCampaignRedemptionIsIdempotentAndBounded(t *testing.T) {
	now := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	c := &liveops.CodeCampaign{Codes: map[string]string{"WELCOME": "coins-100", "SECOND": "gem-1"}, PerAccount: 1}
	if reward, err := c.Redeem(now, "a1", " welcome "); err != nil || reward != "coins-100" {
		t.Fatalf("first redemption = %q, %v", reward, err)
	}
	if _, err := c.Redeem(now, "a1", "WELCOME"); !errors.Is(err, liveops.ErrCodeUsed) {
		t.Fatalf("repeat = %v", err)
	}
	if _, err := c.Redeem(now, "a1", "SECOND"); !errors.Is(err, liveops.ErrCodeLimit) {
		t.Fatalf("limit = %v", err)
	}
	c.Ends = now
	if _, err := c.Redeem(now, "a2", "SECOND"); !errors.Is(err, liveops.ErrCodeExpired) {
		t.Fatalf("expiry = %v", err)
	}
}
