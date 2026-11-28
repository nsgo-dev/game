package liveops

import (
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrCodeUnknown = errors.New("liveops: unknown code")
	ErrCodeUsed    = errors.New("liveops: code already used")
	ErrCodeExpired = errors.New("liveops: code expired")
	ErrCodeLimit   = errors.New("liveops: account code limit reached")
)

// CodeCampaign is an in-memory idempotent redemption policy. Durable services should persist the same claim key transactionally with the reward.
type CodeCampaign struct {
	mu         sync.Mutex
	Codes      map[string]string
	Ends       time.Time
	PerAccount int
	claimed    map[string]map[string]bool
}

// Redeem claims code for account and returns its reward identifier. Codes are case-insensitive and each account can claim a campaign at most PerAccount times.
func (c *CodeCampaign) Redeem(now time.Time, account, code string) (string, error) {
	if c == nil {
		return "", ErrCodeUnknown
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.Ends.IsZero() && !now.Before(c.Ends) {
		return "", ErrCodeExpired
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	reward, ok := c.Codes[code]
	if !ok {
		return "", ErrCodeUnknown
	}
	if c.claimed == nil {
		c.claimed = map[string]map[string]bool{}
	}
	claims := c.claimed[account]
	if claims == nil {
		claims = map[string]bool{}
		c.claimed[account] = claims
	}
	if claims[code] {
		return "", ErrCodeUsed
	}
	if c.PerAccount > 0 && len(claims) >= c.PerAccount {
		return "", ErrCodeLimit
	}
	claims[code] = true
	return reward, nil
}
