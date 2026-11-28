package releaseplan

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/pkg/project"
)

var refusedResourceNames = map[string]struct{}{
	".aws": {}, ".azure": {}, ".docker": {}, ".git": {}, ".gnupg": {}, ".kube": {}, ".npmrc": {},
	".netrc": {}, ".pypirc": {}, ".pulumi": {}, ".ssh": {}, ".terraform": {}, ".yarnrc": {},
	".git-credentials": {}, "id_rsa": {}, "id_ed25519": {},
}

// CheckResources checks files that would be copied from resources/ into a
// release package. It returns one readiness item per finding, or one OK item
// when the directory is clean. Allowed dotfiles are warnings so a person can
// confirm that they are intentional before publishing.
func CheckResources(dir string, f *project.File) []ReadinessItem {
	root := filepath.Join(dir, "resources")
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return []ReadinessItem{{ID: "resources", Label: "Resources", Detail: "resources/ is absent", Status: ReadinessOK}}
	}
	if err != nil {
		return []ReadinessItem{{ID: "resources", Label: "Resources", Detail: fmt.Sprintf("could not inspect resources/: %v", err), Status: ReadinessFail}}
	}
	if !info.IsDir() {
		return []ReadinessItem{{ID: "resources", Label: "Resources", Detail: "resources is not a directory", Status: ReadinessFail}}
	}
	allowed := make(map[string]struct{})
	if f != nil && f.Export != nil {
		for _, p := range f.Export.AllowResources {
			allowed[filepath.ToSlash(filepath.Clean(p))] = struct{}{}
		}
	}
	var items []ReadinessItem
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			items = append(items, resourceFinding(path, "cannot inspect resource: "+walkErr.Error(), ReadinessFail))
			return nil
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		base := strings.ToLower(entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			items = append(items, resourceFinding(rel, "symbolic links are refused in release resources", ReadinessFail))
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			if _, ok := allowed[rel]; ok {
				items = append(items, resourceFinding(rel, "allowed dotfile or dot-directory requires a release review", ReadinessWarn))
				return nil
			}
			items = append(items, resourceFinding(rel, "dotfiles and dot-directories are refused in release resources", ReadinessFail))
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if _, refused := refusedResourceNames[base]; refused || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".p8") || strings.HasSuffix(base, ".p12") || strings.HasSuffix(base, ".pfx") || strings.HasSuffix(base, ".jks") || strings.HasSuffix(base, ".keystore") || strings.HasSuffix(base, ".mobileprovision") || strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") {
			items = append(items, resourceFinding(rel, "credential or signing-key file name is refused in release resources", ReadinessFail))
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr == nil && (bytes.Contains(data, []byte("-----BEGIN PRIVATE KEY-----")) || bytes.Contains(data, []byte("-----BEGIN RSA PRIVATE KEY-----")) || bytes.Contains(data, []byte("-----BEGIN EC PRIVATE KEY-----"))) {
			items = append(items, resourceFinding(rel, "PEM private-key content is refused in release resources", ReadinessFail))
		}
		if readErr == nil && bytes.Contains(data, []byte(`"type"`)) && bytes.Contains(data, []byte(`service_account`)) && bytes.Contains(data, []byte(`private_key`)) {
			items = append(items, resourceFinding(rel, "Google service-account credentials are refused in release resources", ReadinessFail))
		}
		return nil
	})
	if err != nil {
		items = append(items, resourceFinding("resources/", "could not walk resources/: "+err.Error(), ReadinessFail))
	}
	if len(items) == 0 {
		return []ReadinessItem{{ID: "resources", Label: "Resources", Detail: "resources/ contains no refused files", Status: ReadinessOK}}
	}
	return items
}

func resourceFinding(path, detail string, status ReadinessStatus) ReadinessItem {
	return ReadinessItem{ID: "resources:" + path, Label: "Resource " + path, Detail: detail, Status: status, RequiresPerson: status == ReadinessWarn}
}
