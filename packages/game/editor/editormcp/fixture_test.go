package editormcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// heroColor is the colour of every pixel of the fixture's sprite image.
var heroColor = color.RGBA{R: 0x40, G: 0x80, B: 0xc0, A: 0xff}

const fixtureProject = `# The fixture's project file.
version: 1
name: Fixture
id: dev.example.fixture
appVersion: 1.0.0
window:
  width: 320
  height: 180
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
    - name: other
      path: scenes/other.scene.yaml
`

const fixtureNSGo = `name: Fixture
id: dev.example.fixture
version: 1.0.0
`

const fixtureAtlas = `{
  "frames": {
    "hero": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}},
    "slime": {"frame": {"x": 16, "y": 0, "w": 16, "h": 16}}
  },
  "meta": {
    "image": "actors.png",
    "size": {"w": 32, "h": 16}
  }
}
`

const fixtureRoom = `version: 2
name: Room
virtualWidth: 320
virtualHeight: 180
layers:
  - name: actors
    objects:
      - id: hero
        name: Hero
        x: 64
        y: 40
        sprite:
          atlas: actors
          frame: hero
        components:
          body:
            speed: 48
  - name: props
`

const fixtureOther = `version: 2
name: Other
layers:
  - name: actors
    objects:
      - id: ghost
        sprite:
          atlas: actors
          frame: ghost
`

// fixture is an editor open on a project on disk.
type fixture struct {
	dir string
	ed  *editor.Editor
}

// scenePath is the path of the project's main scene file.
func (f fixture) scenePath() string { return filepath.Join(f.dir, "scenes", "room.scene.yaml") }

// atlasPNG encodes the fixture's 32×16 sprite sheet.
func atlasImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 32, 16))
	for y := range 16 {
		for x := range 32 {
			img.SetRGBA(x, y, heroColor)
		}
	}
	return img
}

func encodePNG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// components registers the fixture's body component.
func components() *sceneformat.ComponentRegistry {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name: "body",
		Fields: []sceneformat.Field{
			{Name: "solid", Kind: sceneformat.FieldBool, Default: true},
			{Name: "speed", Kind: sceneformat.FieldFloat, Default: 32.0, Min: sceneformat.Float64(0), Max: sceneformat.Float64(500)},
		},
	})
	return reg
}

// writeProject writes the fixture project into dir.
func writeProject(t testing.TB, dir string) {
	t.Helper()
	files := map[string][]byte{
		"game.yaml":               []byte(fixtureProject),
		"nsgo.yaml":               []byte(fixtureNSGo),
		"art/actors.json":         []byte(fixtureAtlas),
		"art/actors.png":          encodePNG(t, atlasImage()),
		"scenes/room.scene.yaml":  canonical(t, fixtureRoom),
		"scenes/other.scene.yaml": []byte(fixtureOther),
		"data/items.yaml":         []byte("version: 1\nrows:\n  - id: sword\n    damage: 3\n"),
	}
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// canonical is scene as the editor saves it, so that a scene opened and not edited reads as saved.
func canonical(t testing.TB, scene string) []byte {
	t.Helper()
	doc, err := sceneformat.Parse([]byte(scene))
	if err != nil {
		t.Fatal(err)
	}
	data, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// newEditor returns an editor with the fixture's resolver and components, on no project.
func newEditor(t testing.TB) *editor.Editor {
	t.Helper()
	ed, err := editor.New(nil, sceneformat.AtlasMap{
		"actors": &sprite.Atlas{
			Image: core.ImageSource{Pixels: atlasImage()},
			Frames: map[string]sprite.Frame{
				"hero":  {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
				"slime": {Name: "slime", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(components()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	return ed
}

// openFixture writes the fixture project and opens it in a new editor.
func openFixture(t testing.TB) fixture {
	t.Helper()
	dir := t.TempDir()
	writeProject(t, dir)
	ed := newEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	return fixture{dir: dir, ed: ed}
}

// endpointDir points the endpoints at a fresh directory short enough for a socket path on every platform, for the rest of the test.
func endpointDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS != "windows" {
		var err error
		if dir, err = os.MkdirTemp("/tmp", "edmcp"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}
	t.Setenv(editormcp.EndpointDirEnv, dir)
	return dir
}

// legacyProtocol is the MCP version before 2026-07-28, under which a subscription is a request that returns rather than a stream that stays open, as clients that subscribe to the change stream use it.
const legacyProtocol = "2025-11-25"

// connect connects an MCP client to s in memory and returns its session, speaking the SDK's latest protocol, or the version before it when opts subscribes to resources.
func connect(t testing.TB, s *editormcp.Server, opts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.Connect(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	var session *mcp.ClientSessionOptions
	if opts != nil && opts.ResourceUpdatedHandler != nil {
		session = &mcp.ClientSessionOptions{ProtocolVersion: legacyProtocol}
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, opts).Connect(context.Background(), ct, session)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call calls the tool name with args and returns its result, failing the test on a protocol error.
func call(t testing.TB, cs *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

// text joins a result's text contents.
func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// mustOK fails the test when res is a tool error, and returns its text.
func mustOK(t testing.TB, res *mcp.CallToolResult) string {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool error: %s", text(res))
	}
	return text(res)
}

// structured decodes a result's structured content into v.
func structured(t testing.TB, res *mcp.CallToolResult, v any) {
	t.Helper()
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("structured content %s: %v", data, err)
	}
}

// pngOf returns a result's image, decoded.
func pngOf(t testing.TB, res *mcp.CallToolResult) image.Image {
	t.Helper()
	for _, c := range res.Content {
		if ic, ok := c.(*mcp.ImageContent); ok {
			if ic.MIMEType != "image/png" {
				t.Fatalf("image type %q, want image/png", ic.MIMEType)
			}
			img, err := png.Decode(bytes.NewReader(ic.Data))
			if err != nil {
				t.Fatal(err)
			}
			return img
		}
	}
	t.Fatalf("no image in %s", text(res))
	return nil
}
