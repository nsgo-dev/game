package accounts

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPublicDirectoryUniqueBatchAndCooldown(t *testing.T) {
	d := NewPublicDirectory()
	now := time.Unix(100, 0)
	d.Now = func() time.Time { return now }
	d.UsernameCooldown = time.Hour
	if _, e := d.Set(context.Background(), "a", ProfileUpdate{Username: "Ａda"}); e != nil {
		t.Fatal(e)
	}
	if _, e := d.Set(context.Background(), "b", ProfileUpdate{Username: "ada"}); !errors.Is(e, ErrTaken) {
		t.Fatalf("taken=%v", e)
	}
	now = now.Add(time.Minute)
	if _, e := d.Set(context.Background(), "a", ProfileUpdate{Username: "new"}); !errors.Is(e, ErrUsernameCooldown) {
		t.Fatalf("cooldown=%v", e)
	}
	ps, e := d.Users(context.Background(), nil, []string{"ada"})
	if e != nil || len(ps) != 1 || ps[0].AccountID != "a" || ps[0].Metadata != nil {
		t.Fatalf("users=%+v %v", ps, e)
	}
}
