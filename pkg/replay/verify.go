package replay

import (
	"errors"
	"fmt"
	"strconv"
)

var ErrVerification = errors.New("replay: verification failed")

// Verify replays rec through sim and compares the returned score and state hash
// with rec.Result. The simulation receives the recording seed and a playback
// ActionSource, and must advance it once per recorded step.
func Verify(rec *Recording, sim func(seed uint64, src ActionSource) (score int64, stateHash string)) error {
	if rec == nil || sim == nil {
		return fmt.Errorf("%w: missing recording or simulation", ErrVerification)
	}
	if err := rec.Validate(); err != nil {
		return err
	}
	score, hash := sim(rec.Seed, NewPlayer(rec))
	if want, ok := rec.Result["score"]; ok {
		got, err := strconv.ParseInt(want, 10, 64)
		if err != nil || got != score {
			return fmt.Errorf("%w: score is %d, recording claims %q", ErrVerification, score, want)
		}
	}
	if want, ok := rec.Result["state_hash"]; ok && want != hash {
		return fmt.Errorf("%w: state hash is %q, recording claims %q", ErrVerification, hash, want)
	}
	return nil
}
