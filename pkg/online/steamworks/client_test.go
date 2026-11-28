package steamworks

import (
	"reflect"
	"testing"
)

type fakeDriver struct{ events []string }

func (f *fakeDriver) Initialize(id uint32) error { f.events = append(f.events, "init"); return nil }
func (f *fakeDriver) Shutdown()                  { f.events = append(f.events, "shutdown") }
func (f *fakeDriver) RunCallbacks()              { f.events = append(f.events, "pump") }
func TestOpenPumpCloseAndConnect(t *testing.T) {
	d := &fakeDriver{}
	c, err := Open(Options{AppID: 42, Driver: d, ConnectArgs: []string{"game", "+connect", "76561198000000000"}})
	if err != nil {
		t.Fatal(err)
	}
	var got Event
	c.On(EventJoinRequested, func(e Event) { got = e })
	if got.Connect != "76561198000000000" {
		t.Fatalf("connect=%q", got.Connect)
	}
	c.Pump()
	_ = c.Close()
	_ = c.Close()
	if !reflect.DeepEqual(d.events, []string{"init", "pump", "shutdown"}) {
		t.Fatalf("events=%v", d.events)
	}
}
func TestConnectArgumentValidation(t *testing.T) {
	if ConnectArgument([]string{"+connect", "76561198000000000"}) == "" {
		t.Fatal("valid connect missing")
	}
	for _, args := range [][]string{{"+connect", "-1"}, {"+connect=abc"}, {"+connect"}} {
		if ConnectArgument(args) != "" {
			t.Fatalf("accepted %v", args)
		}
	}
}
