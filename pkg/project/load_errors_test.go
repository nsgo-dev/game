package project_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/project"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("project unknown field", func(t *testing.T) {
		data := []byte("version: 1\nname: Demo\nextra: true\n")
		_, err := project.Parse(data)
		loaderrtest.Expect(t, err, "", 3)
	})

	t.Run("project unsupported version", func(t *testing.T) {
		data := []byte("version: 2\nname: Demo\n")
		_, err := project.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})

	t.Run("brief unsupported version", func(t *testing.T) {
		data := []byte("version: 2\n")
		_, err := project.ParseBrief(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})

	const head = "version: 1\nname: Demo\nid: dev.nsgo.demo\nappVersion: 0.1.0\nwindow: {width: 320, height: 180}\n"

	t.Run("audio concurrency invalid resolve", func(t *testing.T) {
		data := []byte(head + "audio:\n  concurrency:\n    - name: hits\n      resolve: random\n")
		_, err := project.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "audio.concurrency[0].resolve")
	})

	t.Run("audio concurrency invalid scope", func(t *testing.T) {
		data := []byte(head + "audio:\n  concurrency:\n    - name: hits\n      scope: local\n")
		_, err := project.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "audio.concurrency[0].scope")
	})

	t.Run("audio concurrency duplicate name", func(t *testing.T) {
		data := []byte(head + "audio:\n  concurrency:\n    - name: hits\n    - name: hits\n")
		_, err := project.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "audio.concurrency[1].name")
	})

	t.Run("audio duck self-ducking", func(t *testing.T) {
		data := []byte(head + "audio:\n  duck:\n    - bus: music\n      by: music\n      amount: 0.4\n")
		_, err := project.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "audio.duck[0].by")
	})

	t.Run("audio duck amount out of range", func(t *testing.T) {
		data := []byte(head + "audio:\n  duck:\n    - bus: music\n      by: dialogue\n      amount: 1.5\n")
		_, err := project.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "audio.duck[0].amount")
	})

	t.Run("audio maxVoices negative", func(t *testing.T) {
		data := []byte(head + "audio:\n  maxVoices: -1\n")
		_, err := project.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "audio.maxVoices")
	})
}
