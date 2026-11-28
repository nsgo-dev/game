package sceneformat

import "testing"

func TestConnectionWhenRoundTrip(t *testing.T) {
	doc := &SceneDoc{Version: CurrentVersion, Name: "rules", Connections: []ConnectionDoc{{From: "coin", Event: "touch", To: "coin", Handler: "scene.destroy", When: "score >= 10"}}}
	if doc.Connections[0].When != "score >= 10" {
		t.Fatal("when expression was not retained")
	}
}
