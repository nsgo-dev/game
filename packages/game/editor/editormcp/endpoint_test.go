package editormcp_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
)

// ProjectRoot finds the project from its game.yaml, its directory or any directory inside it, and Endpoint gives every spelling the same endpoint.
func TestProjectRootAndEndpoint(t *testing.T) {
	endpointDir(t)
	dir := t.TempDir()
	writeProject(t, dir)
	want, err := editormcp.ProjectRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(dir, "game.yaml"), filepath.Join(dir, "scenes"), filepath.Join(dir, "scenes", ".")} {
		got, err := editormcp.ProjectRoot(p)
		if err != nil || got != want {
			t.Errorf("ProjectRoot(%s) = %q, %v; want %q", p, got, err, want)
		}
	}
	if _, err := editormcp.ProjectRoot(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no game.yaml") {
		t.Errorf("ProjectRoot outside a project: %v", err)
	}
	a, err := editormcp.Endpoint(want)
	if err != nil {
		t.Fatal(err)
	}
	b, err := editormcp.Endpoint(want + string(filepath.Separator))
	if err != nil || a != b || filepath.Ext(a) != ".sock" {
		t.Fatalf("endpoints %q and %q (%v)", a, b, err)
	}
	other, _ := editormcp.Endpoint(filepath.Join(want, "other"))
	if other == a {
		t.Fatal("two projects share an endpoint")
	}

	t.Setenv(editormcp.EndpointDirEnv, "/"+strings.Repeat("x", 120))
	if _, err := editormcp.Endpoint(want); err == nil || !strings.Contains(err.Error(), editormcp.EndpointDirEnv) {
		t.Fatalf("a too long endpoint: %v", err)
	}
}

// Dial without an editor says how to open the project; Listen refuses a second editor on the same project and replaces an endpoint an editor left behind.
func TestListenAndDial(t *testing.T) {
	dir := endpointDir(t)
	root := t.TempDir()
	_, err := editormcp.Dial(context.Background(), root)
	if !errors.Is(err, editormcp.ErrNoEditor) || !strings.Contains(err.Error(), "go tool game editor "+root) {
		t.Fatalf("Dial without an editor: %v", err)
	}
	l, err := editormcp.Listen(root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(l.Addr().String()) != dir {
		t.Fatalf("endpoint %s is not in %s", l.Addr(), dir)
	}
	if info, err := os.Stat(l.Addr().String()); err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("endpoint mode %v: %v, want only the user's", info.Mode(), err)
	}
	accepted := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
			_ = c.Close()
		}
		accepted <- err
	}()
	conn, err := editormcp.Dial(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
	if _, err := editormcp.Listen(root); !errors.Is(err, editormcp.ErrInUse) {
		t.Fatalf("a second Listen: %v", err)
	}
	path := l.Addr().String()
	_ = l.Close()
	_ = os.Remove(path)

	// An editor that quit without closing its endpoint leaves the file behind, and nothing answers on it.
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l, err = editormcp.Listen(root)
	if err != nil {
		t.Fatalf("Listen over a left endpoint: %v", err)
	}
	_ = l.Close()
}

// Open serves the project open in the editor and follows the editor to another project; Close takes the endpoint down.
func TestServerOpenFollowsTheProject(t *testing.T) {
	endpointDir(t)
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	first, err := s.Status()
	if err != nil || first == "" {
		t.Fatalf("Status = %q, %v", first, err)
	}
	root, _ := editormcp.ProjectRoot(f.dir)
	if want, _ := editormcp.Endpoint(root); want != first {
		t.Fatalf("serves on %s, want %s", first, want)
	}

	// An agent connected for the first project.
	first0, err := editormcp.Dial(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := mcp.NewClient(&mcp.Implementation{Name: "agent"}, nil).Connect(context.Background(), &mcp.IOTransport{Reader: first0, Writer: first0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()

	second := t.TempDir()
	writeProject(t, second)
	if err := f.ed.OpenProject(second); err != nil {
		t.Fatal(err)
	}
	f.ed.WaitForProblems()
	now, err := s.Status()
	if err != nil || now == first || now == "" {
		t.Fatalf("after opening another project: %q, %v (was %q)", now, err, first)
	}
	if _, err := editormcp.Dial(context.Background(), root); !errors.Is(err, editormcp.ErrNoEditor) {
		t.Fatalf("the first project's endpoint still answers: %v", err)
	}
	// The first project's agent is cut off rather than left editing the second project.
	_ = agent.Wait()
	conn, err := editormcp.Dial(context.Background(), mustRoot(t, second))
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	// A second editor on the same project cannot serve it, and says why.
	other := newEditor(t)
	if err := other.OpenProject(second); err != nil {
		t.Fatal(err)
	}
	other.WaitForProblems()
	s2 := editormcp.New(other, editormcp.Options{})
	if err := s2.Open(); !errors.Is(err, editormcp.ErrInUse) {
		t.Fatalf("a second editor's Open: %v", err)
	}
	_ = s2.Close()

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(now); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Close left %s: %v", now, err)
	}
}

func mustRoot(t *testing.T, dir string) string {
	t.Helper()
	root, err := editormcp.ProjectRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}
