package editor_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestBuiltInProviderSettingsRoundTripAndModelOverrides(t *testing.T) {
	settings := &editor.EditorSettings{Version: 1, Agent: &editor.AgentSettings{BuiltInFormat: editor.BuiltInFormatOpenAI, BuiltInEndpoint: "http://127.0.0.1:11434/v1", BuiltInModel: "local-main", BuiltInFastModel: "local-fast"}}
	data, err := editor.EncodeEditorSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	got, err := editor.ParseEditorSettings(data)
	if err != nil {
		t.Fatal(err)
	}
	if a := got.Agent; a == nil || a.BuiltInFormat != editor.BuiltInFormatOpenAI || a.BuiltInEndpoint != "http://127.0.0.1:11434/v1" || a.BuiltInModel != "local-main" || a.BuiltInFastModel != "local-fast" {
		t.Fatalf("agent settings = %+v", got.Agent)
	}
	again, err := editor.EncodeEditorSettings(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Fatalf("round trip changed editor.yaml:\n%s\nwant:\n%s", again, data)
	}

	ed := newTestEditor(t)
	if err := ed.ApplySettings(*got); err != nil {
		t.Fatal(err)
	}
	if m := ed.AgentBuiltInModelForRoute(editor.AgentRouteBuiltInDefault); m != "local-main" {
		t.Fatalf("default route model = %q, want local-main", m)
	}
	if m := ed.AgentBuiltInModelForRoute(editor.AgentRouteBuiltInFast); m != "local-fast" {
		t.Fatalf("fast route model = %q, want local-fast", m)
	}
	if ep := ed.AgentBuiltInEndpointForSettings(); ep != "http://127.0.0.1:11434/v1" {
		t.Fatalf("endpoint = %q", ep)
	}
	if err := ed.WithAgentActor(func() error {
		s := ed.Settings()
		s.Agent.BuiltInEndpoint = "https://attacker.example/v1"
		return ed.ApplySettings(s)
	}); !errors.Is(err, editor.ErrAgentSettingsYouOnly) {
		t.Fatalf("agent changing built-in endpoint = %v, want ErrAgentSettingsYouOnly", err)
	}
}

func TestBuiltInProviderSettingsRejectInvalidValues(t *testing.T) {
	for _, doc := range []string{
		"version: 1\nagent:\n  builtInFormat: llama\n",
		"version: 1\nagent:\n  builtInEndpoint: http://models.example/v1\n",
		"version: 1\nagent:\n  builtInModel: \"bad\\nmodel\"\n",
	} {
		if _, err := editor.ParseEditorSettings([]byte(doc)); err == nil {
			t.Fatalf("ParseEditorSettings accepted %q", doc)
		}
	}
}

// The runner sends requests to agent.builtInEndpoint with the configured model, ahead of Editor.BuiltInAgentEndpoint.
func TestBuiltInAgentUsesConfiguredEndpointAndModel(t *testing.T) {
	var fieldHits atomic.Int32
	fieldSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fieldHits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer fieldSrv.Close()

	var mu sync.Mutex
	var models []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		models = append(models, body.Model)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_provider_settings",
			"model":       body.Model,
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "Done."}},
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 2},
		})
	}))
	defer srv.Close()

	ed := newTestEditor(t)
	ed.BuiltInAgentEndpoint = fieldSrv.URL
	ed.BuiltInHTTPClient = srv.Client()
	s := ed.Settings()
	s.Agent = &editor.AgentSettings{BuiltInEndpoint: srv.URL + "/v1/messages", BuiltInFastModel: "local-fast"}
	if err := ed.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Secrets(map[string]string{
		editor.AgentAPIKeySecretAccount: "sk-ant-provider-settings-secret",
	}))

	if got := ed.ResolveAgentModelRoute(editor.AgentTaskPlaytest); got != editor.AgentRouteBuiltInFast {
		t.Fatalf("playtest route = %q, want %q", got, editor.AgentRouteBuiltInFast)
	}
	if err := ed.SubmitAgentTaskPrompt(editor.AgentTaskPlaytest, "Summarize the playtest"); err != nil {
		t.Fatalf("SubmitAgentTaskPrompt: %v", err)
	}
	app.WaitUntil(5*time.Second, func() bool { return !ed.AgentWorking() })
	app.Settle()

	mu.Lock()
	defer mu.Unlock()
	if len(models) == 0 || models[0] != "local-fast" {
		t.Fatalf("models sent to configured endpoint = %v, want local-fast", models)
	}
	if fieldHits.Load() != 0 {
		t.Fatalf("Editor.BuiltInAgentEndpoint received %d requests, want 0", fieldHits.Load())
	}
}

// Until the OpenAI-compatible adapter lands, the runner refuses openai instead of sending Anthropic Messages requests to it.
func TestBuiltInAgentRefusesUnsupportedFormat(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ed := newTestEditor(t)
	ed.AgentSecretStore = editor.NewMemorySecretStore(map[string]string{
		editor.AgentAPIKeySecretAccount: "sk-ant-provider-format-secret",
	})
	ed.BuiltInHTTPClient = srv.Client()
	s := ed.Settings()
	s.Agent = &editor.AgentSettings{BuiltInFormat: editor.BuiltInFormatOpenAI, BuiltInEndpoint: srv.URL + "/v1"}
	if err := ed.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	if err := ed.SubmitAgentTaskPrompt(editor.AgentTaskPlaytest, "test"); !errors.Is(err, editor.ErrBuiltInFormatUnsupported) {
		t.Fatalf("SubmitAgentTaskPrompt = %v, want ErrBuiltInFormatUnsupported", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("endpoint received %d requests, want 0", hits.Load())
	}
}
