package editor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProjectPathOnDiskUsesStoredNames checks that a component the file system opens under another spelling is judged by the name stored in its directory, which is how an NTFS 8.3 short name or a normalization variant of a secret name is still refused.
func TestProjectPathOnDiskUsesStoredNames(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".nsgo", "threads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".nsgo", "threads", "t1.yaml"), []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// ASCII components and missing components are left as given.
	if got := projectPathOnDisk(root, "scenes/missing~1/a.yaml"); got != "scenes/missing~1/a.yaml" {
		t.Fatalf("projectPathOnDisk(missing) = %q", got)
	}
	if got := projectPathOnDisk(root, ".nsgo/threads/t1.yaml"); got != ".nsgo/threads/t1.yaml" {
		t.Fatalf("projectPathOnDisk(stored) = %q", got)
	}
	// On a case-insensitive file system (APFS, NTFS) U+017F LONG S opens the stored ".nsgo", and the walk returns the stored name; a case-sensitive one opens nothing, and the fold in ClassifyProjectFilePrivacy covers it instead.
	variant := ".nſgo/threads/t1.yaml"
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(variant))); err != nil {
		t.Skipf("this file system does not open %q as .nsgo: %v", variant, err)
	}
	if got := projectPathOnDisk(root, variant); got != ".nsgo/threads/t1.yaml" {
		t.Fatalf("projectPathOnDisk(%q) = %q, want the stored name .nsgo/threads/t1.yaml", variant, got)
	}
	if !IsSecretProjectFile(root, variant) {
		t.Fatalf("IsSecretProjectFile(%q) = false", variant)
	}
}
