// Package rhythm keeps a rhythm game in time with its music: a beat map read from YAML, a music clock that reads the track's position and beat at any engine time, and a calibration of each device's audio and video latency.
//
// # Beat maps
//
// A BeatMap is a track's tempo map in a versioned YAML file: where beat 0 falls in the audio, tempo changes at any beat and meter changes on bar lines. ParseBeatMap refuses unknown fields and EncodeBeatMap writes the fields in a fixed order, so a map round-trips unchanged. BeatMap.Timing turns it into a Timing that converts track time to beats and bars and back, at float64 precision from each change's own start, so a beat's time never drifts with the length of the track.
//
// # The music clock
//
// A Clock runs on the engine's clock, audio.Now, which audio.Voice.StartAt also counts on. Start it at the time a track starts playing, and it extrapolates the output's position from there; Sync, fed from an audio.Player's OnProgress, moves it only when the player drifts past a tolerance. Position and Beat are what the player hears, corrected by the device's Offsets, and judge input; DrawPosition and DrawBeat add the video offset for the frame being drawn; BeatTime is when to start a hit sound or a metronome click with audio.Voice.StartAt.
//
// # Calibration
//
// A Calibrator matches taps with cues every Interval and measures the median offset and its spread. A Calibration runs one on the engine: clicks scheduled ahead with audio.Voice.StartAt for the audio offset, seeded with audio.OutputLatency, or silent cues to flash for the video offset. CalibrationScreen is the gameui screen a player calibrates on, and OffsetStore keeps each device's Offsets as a versioned YAML document in core.Storage, keyed by DeviceKey.
package rhythm
