package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/editor"
)

func TestEditorYAMLLoadErrors(t *testing.T) {
	t.Run("recent projects unknown field", func(t *testing.T) {
		data := []byte("version: 1\nextra: true\nprojects: []\n")
		_, err := editor.ParseRecentProjects(data)
		loaderrtest.Expect(t, err, "", 2)
	})

	t.Run("checkpoint manifest invalid who", func(t *testing.T) {
		data := []byte("version: 1\nid: cp1\ncreatedAt: 2026-11-28T00:00:00Z\nwho: bot\nkind: turn\ntitle: Edit\n")
		_, err := editor.ParseCheckpointManifest(data)
		loaderrtest.ExpectField(t, err, "", data, "who")
	})

	t.Run("layout invalid axis", func(t *testing.T) {
		data := []byte("version: 1\ncenter:\n  axis: diagonal\n  children:\n    - panels: [viewport]\n")
		_, err := editor.ParseLayout(data)
		loaderrtest.ExpectField(t, err, "", data, "center.axis")
	})

	t.Run("agent thread invalid id", func(t *testing.T) {
		data := []byte("version: 1\nid: ../bad\ntasks: []\n")
		_, err := editor.ParseAgentThread(data)
		loaderrtest.ExpectField(t, err, "", data, "id")
	})
}
