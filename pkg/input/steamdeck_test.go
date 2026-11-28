package input

import "testing"

func TestDetectSteamDeckGamepad(t *testing.T) {
	for _, name := range []string{"Steam Deck Controller", "Valve SteamDeck", "Valve Handheld"} {
		if got := DetectGamepadKind(name); got != DeviceGamepadSteamDeck || got.String() != "steam_deck" || !got.IsGamepad() {
			t.Fatalf("DetectGamepadKind(%q) = %v", name, got)
		}
	}
}

func TestSteamDeckPromptsFallBackToXbox(t *testing.T) {
	var tracker DeviceTracker
	tracker.OnGamepadActivity("Steam Deck")
	if got := tracker.LastDeviceUntracked(); got != DeviceGamepadSteamDeck {
		t.Fatalf("LastDevice = %v, want steam_deck", got)
	}

	pt := DefaultPromptTable()
	if got := pt.PromptFor("jump", &tracker); got.Label != "[A]" || got.Icon != "gamepad_xbox_a" {
		t.Fatalf("Deck jump prompt = %+v, want the Xbox [A] prompt", got)
	}

	pt.Set("jump", DeviceGamepadSteamDeck, Prompt{Label: "[A]", Icon: "gamepad_deck_a", Glyph: "A"})
	if got := pt.PromptFor("jump", &tracker); got.Icon != "gamepad_deck_a" {
		t.Fatalf("Deck jump prompt = %+v, want the registered Deck prompt", got)
	}

	pt.Set("only_generic", DeviceGamepadGeneric, Prompt{Label: "[South]"})
	if got := pt.Prompt("only_generic", DeviceGamepadSteamDeck); got.Label != "[South]" {
		t.Fatalf("Deck prompt without Deck or Xbox entries = %+v, want the generic one", got)
	}
}
