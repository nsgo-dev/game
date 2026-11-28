package editormcp

import "testing"

func TestValidatePlayStartReplayFile(t *testing.T) {
	for _, tc := range []struct {
		path string
		ok   bool
	}{{"replays/run.replay.yaml", true}, {"../escape.replay.yaml", false}, {"/abs.replay.yaml", false}, {"replays/run.yaml", false}, {"replays/../run.replay.yaml", false}} {
		err := ValidatePlayStart(PlayStartArgs{ReplayFile: tc.path})
		if (err == nil) != tc.ok {
			t.Errorf("%q err=%v", tc.path, err)
		}
	}
}
