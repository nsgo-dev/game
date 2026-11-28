package input

import "testing"

func TestDetectSteamDeckGamepad(t *testing.T) {
	for _, name := range []string{"Steam Deck Controller", "Valve SteamDeck", "Valve Handheld"} {
		if got := DetectGamepadKind(name); got != DeviceGamepadSteamDeck || got.String() != "steam_deck" || !got.IsGamepad() {
			t.Fatalf("DetectGamepadKind(%q) = %v", name, got)
		}
	}
}

func TestSteamDeckPromptOverridesGenericGamepad(t *testing.T) {
	prompts := NewPromptTable()
	prompts.Set("confirm", DeviceGamepadGeneric, Prompt{Label: "[A]", Glyph: "A"})
	prompts.Set("confirm", DeviceGamepadSteamDeck, Prompt{Label: "[A]", Glyph: "Deck A"})
	if got := prompts.Prompt("confirm", DeviceGamepadSteamDeck); got.Glyph != "Deck A" {
		t.Fatalf("Steam Deck prompt = %+v, want Deck A", got)
	}
}
