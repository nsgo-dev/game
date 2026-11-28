package editor_test

import (
	"github.com/nsgo-dev/game/pkg/editor"
	"testing"
)

func TestBuiltInProviderSettingsRoundTripAndModelOverrides(t *testing.T) {
	settings := &editor.EditorSettings{Version: 1, Language: editor.LanguageSystem, Appearance: editor.AppearanceSystem, Agent: &editor.AgentSettings{BuiltInFormat: editor.BuiltInFormatOpenAI, BuiltInEndpoint: "http://127.0.0.1:11434/v1", BuiltInModel: "local-main", BuiltInFastModel: "local-fast"}}
	data, err := editor.EncodeEditorSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	got, err := editor.ParseEditorSettings(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent == nil || got.Agent.BuiltInFormat != editor.BuiltInFormatOpenAI || got.Agent.BuiltInModel != "local-main" {
		t.Fatalf("agent settings = %+v", got.Agent)
	}
}

func TestBuiltInProviderSettingsRejectUnknownFormat(t *testing.T) {
	_, err := editor.ParseEditorSettings([]byte("version: 1\nagent:\n  builtInFormat: llama\n"))
	if err == nil {
		t.Fatal("unknown built-in format accepted")
	}
}
