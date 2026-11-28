package a11y_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/nsgo/core"
)

// ContrastRatio measures text against its background, and EnsureContrast moves a color that falls short of WCAG AA (4.5:1) until it meets it.
func ExampleEnsureContrast() {
	bg := core.RGB(0x202020)
	fg := core.RGB(0x505050)
	fmt.Printf("before: %.2f\n", a11y.ContrastRatio(fg, bg))
	fixed := a11y.EnsureContrast(fg, bg, 4.5)
	fmt.Println("meets AA:", a11y.ContrastRatio(fixed, bg) >= 4.5)
	// Output:
	// before: 2.02
	// meets AA: true
}

// Simulate shows how a color reads under a color-vision deficiency, so a game can check that two of its colors stay apart.
func ExampleSimulate() {
	red, green := core.RGB(0xd03030), core.RGB(0x30a030)
	mode, ok := a11y.ParseColorVision("deuteranopia")
	fmt.Println(mode, ok)
	fmt.Printf("%08x %08x\n", uint32(a11y.Simulate(red, mode)), uint32(a11y.Simulate(green, mode)))
	// Output:
	// deuteranopia true
	// 897b29ff 96883bff
}

// PaletteFor returns semantic colors that stay distinguishable under the chosen color-vision mode.
func ExamplePaletteFor() {
	for _, mode := range []a11y.ColorVision{a11y.VisionNormal, a11y.VisionHighContrast} {
		p := a11y.PaletteFor(mode)
		fmt.Printf("%v: player %08x enemy %08x\n", mode, uint32(p.Player), uint32(p.Enemy))
	}
	// Output:
	// normal: player 56b4e9ff enemy f06430ff
	// high-contrast: player 00e5ffff enemy ff5252ff
}

// Assist keeps a player's assist options in the app's storage; ScaleDT applies the slow-motion setting to a fixed step.
func ExampleAssist() {
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()

	assist := a11y.NewAssist(root, "assist")
	if err := assist.Apply(a11y.AssistOptions{GameSpeed: 0.5, CaptionsEnabled: true, TextScale: 1.5, ColorVision: a11y.VisionProtanopia}); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(assist.ScaleDT(time.Second / 60))
	fmt.Println(assist.CaptionsEnabled(), assist.TextScale(), assist.ColorVision())

	// A fresh Assist on the same storage reads the saved options back.
	again := a11y.NewAssist(root, "assist")
	fmt.Println(again.GameSpeed())
	// Output:
	// 8.333333ms
	// true 1.5 protanopia
	// 0.5
}

// Caption.Format places directional indicators outside the SFX brackets at a pan magnitude of 0.25 or greater.
func ExampleCaption_Format() {
	for _, direction := range []float32{-0.25, 0.24, 0.25} {
		caption := a11y.Caption{Text: "Explosion", Kind: a11y.CaptionSFX, Direction: direction}
		fmt.Println(caption.Format())
	}
	// Output:
	// [<] [Explosion]
	// [Explosion]
	// [Explosion] [>]
}

// A CaptionTrack holds timed captions that Update steps from the game loop; Format adds the speaker and the direction a sound came from.
func ExampleCaptionTrack() {
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()

	track := a11y.NewCaptionTrack(root)
	track.ShowDialogue("Ada", "Over here!", 2*time.Second)
	track.ShowSFXAt("Footsteps", -1, time.Second)
	for _, c := range track.Active() {
		fmt.Printf("%v: %s\n", c.Kind, c.Format())
	}
	track.Update(1500 * time.Millisecond)
	fmt.Println("after 1.5 s:", len(track.Active()))
	// Output:
	// dialogue: Ada: Over here!
	// sfx: [<] [Footsteps]
	// after 1.5 s: 1
}
