package editor_test

import (
	"context"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/flow"
	gamemcp "github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestRemoteInspectorListsAndEditsVars(t *testing.T) {
	doc, err := flow.ParseVars([]byte(`version: 1
vars:
  - name: bridge_repaired
    kind: bool
    doc: East bridge fixed.
  - name: gold
    kind: int
    default: 50
    min: 0
    max: 500
  - name: hero_name
    kind: string
    default: Aria
`))
	if err != nil {
		t.Fatal(err)
	}
	vars, err := flow.NewVars(doc)
	if err != nil {
		t.Fatal(err)
	}
	vars.Self("chest_01").SetBool("opened", true)

	_, sceneDoc := newFixtureRemoteGame(t)
	ed, err := editor.New(sceneDoc, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.RemoteRefreshInterval = 5 * time.Second
	t.Cleanup(func() { _ = ed.Close() })

	bridge := gamemcp.New(gamemcp.Config{Vars: vars})
	srv := editor.NewRemoteBridgeServer(bridge, nil)

	serverT, clientT := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srvSession, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("srv.Connect: %v", err)
	}
	t.Cleanup(func() { _ = srvSession.Close() })

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	if err := ed.ConnectRemote(context.Background(), clientT); err != nil {
		t.Fatalf("ConnectRemote: %v", err)
	}
	app.Settle()

	snap := ed.RemoteSnapshot()
	if !snap.HasVars || len(snap.Vars) != 3 {
		t.Fatalf("RemoteSnapshot vars = (HasVars:%v, len:%d)", snap.HasVars, len(snap.Vars))
	}
	if snap.SelfVars["chest_01"]["opened"] != true {
		t.Fatalf("RemoteSnapshot SelfVars = %+v", snap.SelfVars)
	}

	if !app.Exists("id:remote-vars-section") {
		t.Fatal("expected remote-vars-section in remote inspector")
	}
	if !app.Exists("id:remote-var-bridge_repaired") || !app.Exists("id:remote-var-gold") || !app.Exists("id:remote-var-hero_name") {
		t.Fatal("expected live controls for declared variables")
	}
	if !app.Exists("id:remote-selfvar-chest_01-opened") {
		t.Fatal("expected self variable row for chest_01.opened")
	}

	// Toggle bool switch and edit int variable live through UI controls.
	app.Press("id:remote-var-bridge_repaired")
	app.Clear("id:remote-var-gold")
	app.Type("id:remote-var-gold", "125\n")
	settleRemoteUntil(t, app, "vars updated live", func() bool {
		b, _ := vars.Bool("bridge_repaired")
		g, _ := vars.Int("gold")
		return b && g == 125
	})

	// Also verify programmatic RemoteSetVar updates string variable.
	res, err := ed.RemoteSetVar("hero_name", "Kael", false)
	if err != nil {
		t.Fatalf("RemoteSetVar: %v", err)
	}
	if res.Value != "Kael" {
		t.Fatalf("RemoteSetVar result = %+v", res)
	}
	if got, _ := vars.String("hero_name"); got != "Kael" {
		t.Fatalf("store hero_name = %q, want Kael", got)
	}
}
