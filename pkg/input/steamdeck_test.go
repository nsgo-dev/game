package input

import "testing"

func TestDetectSteamDeckGamepad(t *testing.T) {
	for _, name := range []string{"Steam Deck Controller", "Valve SteamDeck", "Valve Handheld"} {
		if got := DetectGamepadKind(name); got != DeviceGamepadSteamDeck || got.String() != "steam_deck" || !got.IsGamepad() {
			t.Fatalf("DetectGamepadKind(%q) = %v", name, got)
		}
	}
}
