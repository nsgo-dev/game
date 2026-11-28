package diaglog

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRedactor(t *testing.T) {
	r := Redactor{Home: `C:\Users\ana`, Project: `/work/game`, Secrets: []string{"sk-secret"}}
	got := r.Redact(`C:\Users\ana /work/game Bearer abc Authorization: xyz OPENAI_API_KEY=foo sk-secret https://example.com/a?q=x ana@example.com`)
	for _, want := range []string{"<home>", "<project>", "<redacted>", "<redacted-env>", "https://example.com/a", "<redacted-email>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing %q", got, want)
		}
	}
}
func TestRotateCapAndConcurrentLines(t *testing.T) {
	d := t.TempDir()
	w, e := NewWriter(d, "editor", Redactor{}, 64, 3)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if e := w.WriteLine(strings.Repeat("x", 10) + " line"); e != nil {
					t.Error(e)
				}
			}
		}(i)
	}
	wg.Wait()
	_ = w.Close()
	files, _ := filepath.Glob(filepath.Join(d, "editor.log*"))
	if len(files) > 3 {
		t.Fatalf("files=%d", len(files))
	}
	for _, p := range files {
		b, _ := os.ReadFile(p)
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if line != "" && !strings.HasSuffix(line, " line") {
				t.Fatalf("partial line %q", line)
			}
		}
	}
}
func TestExportAndManifest(t *testing.T) {
	d := t.TempDir()
	w, _ := NewWriter(d, "editor", Redactor{Secrets: []string{"secret"}}, 1024, 3)
	_ = w.WriteLine("secret")
	_ = w.Close()
	out := filepath.Join(d, "diag.zip")
	if e := Export(d, out, Manifest{Version: 1, Entries: []Entry{{Name: "editor.log", Size: 1}}}, Redactor{Secrets: []string{"secret"}}); e != nil {
		t.Fatal(e)
	}
	z, e := zip.OpenReader(out)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		_ = r.Close()
		if strings.Contains(string(b), "secret") {
			t.Fatal("secret leaked")
		}
	}
}
