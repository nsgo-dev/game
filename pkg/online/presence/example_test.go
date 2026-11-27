package presence_test

import (
	"context"
	"fmt"

	"github.com/nsgo-dev/game/pkg/online/presence"
)

type printProvider struct {
	name string
}

func (p printProvider) Name() string { return p.name }

func (p printProvider) Set(_ context.Context, a presence.Activity) error {
	fmt.Printf("%s: %s (party %d/%d)\n", p.name, a.Summary(), a.PartySize, a.PartyMax)
	return nil
}

func (p printProvider) Clear(context.Context) error {
	fmt.Printf("%s: cleared\n", p.name)
	return nil
}

func (p printProvider) Close() error { return nil }

func ExampleManager_SetLevel() {
	mgr := presence.NewManager(
		presence.ManagerConfig{},
		printProvider{name: "steam"},
		printProvider{name: "discord"},
	)
	defer mgr.Close()

	ctx := context.Background()
	_ = mgr.Update(ctx, presence.Activity{
		Mode:      "Campaign",
		Level:     "Forest 1-2",
		PartyID:   "squad-1",
		PartySize: 2,
		PartyMax:  4,
	})
	_ = mgr.SetLevel(ctx, "Cavern 2-1")
	_ = mgr.Clear(ctx)

	// Output:
	// steam: Campaign - Forest 1-2 (party 2/4)
	// discord: Campaign - Forest 1-2 (party 2/4)
	// steam: Campaign - Cavern 2-1 (party 2/4)
	// discord: Campaign - Cavern 2-1 (party 2/4)
	// steam: cleared
	// discord: cleared
}
