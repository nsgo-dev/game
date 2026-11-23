package main

import (
	"encoding/json"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const recordsKey = savePrefix + ".slot." + recordsSlot

// empty is an app that shows nothing, for tests that need only its storage.
func empty(*core.Scope) core.Node { return core.View{} }

func TestRecordsSurviveALaunch(t *testing.T) {
	app := nsgotest.Start(t, empty)
	st := newRecordsStore(app.Storage())
	if got, _ := loadRecords(st); got != (records{}) {
		t.Fatalf("first launch records = %+v, want none", got)
	}

	r := records{}
	if !r.add(4) || r.add(2) || r.add(4) {
		t.Fatal("add reported a new best wrongly")
	}
	if err := st.Save(recordsSlot, r); err != nil {
		t.Fatal(err)
	}

	again, _ := loadRecords(newRecordsStore(app.Storage()))
	if again != (records{Best: 4, Games: 3}) {
		t.Fatalf("records after a launch = %+v, want best 4 of 3 games", again)
	}
}

func TestRecordsSlotIsAVersionedEnvelope(t *testing.T) {
	app := nsgotest.Start(t, empty)
	if err := newRecordsStore(app.Storage()).Save(recordsSlot, records{Best: 7, Games: 2}); err != nil {
		t.Fatal(err)
	}
	raw, ok := app.Storage().GetItem(recordsKey)
	if !ok {
		t.Fatalf("nothing stored at %q", recordsKey)
	}
	var envelope struct {
		Version int     `json:"version"`
		Data    records `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("slot is not JSON: %v", err)
	}
	if envelope.Version != recordsVersion || envelope.Data != (records{Best: 7, Games: 2}) {
		t.Fatalf("slot = %s", raw)
	}
}

func TestUnreadableRecordsStartAfresh(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		newer     bool
	}{
		{"corrupt", "{not json", false},
		{"future", `{"version":99,"data":{"best":5,"games":1}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := nsgotest.Start(t, empty, nsgotest.Storage(map[string]string{recordsKey: tc.raw}))
			got, newer := loadRecords(newRecordsStore(app.Storage()))
			if got != (records{}) || newer != tc.newer {
				t.Fatalf("records = %+v, newer %v; want none, newer %v", got, newer, tc.newer)
			}
		})
	}
}
