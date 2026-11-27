package dialogue_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/dialogue"
)

const shop = `title: Shop
---
<<declare $gold = 15>>
Merchant: You have {$gold} gold.
-> Buy a potion (10 gold) <<if $gold >= 10>>
    <<set $gold -= 10>>
    <<give_item potion 1>>
    Merchant: Here you go.
-> Buy a sword (50 gold) <<if $gold >= 50>>
    Merchant: A fine choice.
-> Leave
===
`

// A Runner steps through a script parsed by ParseYarn, returning lines and choices and running game commands.
func ExampleRunner() {
	script, err := dialogue.ParseYarn(shop)
	if err != nil {
		fmt.Println(err)
		return
	}
	r := dialogue.NewRunner(script, dialogue.Options{
		OnCommand: func(e dialogue.Event) { fmt.Println("command:", e.Command, e.Args) },
	})
	step, _ := r.Start("Shop")
	fmt.Printf("%s: %s\n", step.Line.Speaker, step.Line.Text)
	step, _ = r.Continue()
	for _, c := range step.Choices {
		fmt.Println(c.Index, c.Text, c.Available)
	}
	step, _ = r.Choose(0)
	fmt.Println(step.Kind, step.Line.Text)
	gold, _ := r.Var("gold")
	fmt.Println("gold:", gold)
	// Output:
	// Merchant: You have 15 gold.
	// 0 Buy a potion (10 gold) true
	// 1 Buy a sword (50 gold) false
	// 2 Leave true
	// command: give_item [potion 1]
	// line Here you go.
	// gold: 5
}
