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
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if walkErr != nil {
			items = append(items, resourceFinding(rel, "cannot inspect resource: "+walkErr.Error(), ReadinessFail))
			return nil
		}
		if path == root {
			return nil
		}
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
		// Only regular files are read: a named pipe or device would block or never end.
		if !entry.Type().IsRegular() {
			items = append(items, resourceFinding(rel, "only regular files and directories are allowed in release resources", ReadinessFail))
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			items = append(items, resourceFinding(rel, "cannot inspect resource: "+infoErr.Error(), ReadinessFail))
			return nil
		}
		// Credentials are small text files; skipping larger media keeps the editor's repeated readiness checks from reading every video and audio file.
		if info.Size() > maxScannedResourceBytes {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			items = append(items, resourceFinding(rel, "cannot read resource: "+readErr.Error(), ReadinessFail))
			return nil
		}
		if containsPEMPrivateKey(data) {
			items = append(items, resourceFinding(rel, "PEM private-key content is refused in release resources", ReadinessFail))
		}
		if bytes.Contains(data, []byte(`"type"`)) && bytes.Contains(data, []byte(`service_account`)) && bytes.Contains(data, []byte(`private_key`)) {
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

// maxScannedResourceBytes bounds the files whose contents are scanned for private keys and service-account credentials; names are checked for every file.
const maxScannedResourceBytes = 1 << 20

// containsPEMPrivateKey reports whether data holds a PEM private-key header of any kind (PKCS#8, RSA, EC, DSA, OpenSSH, encrypted).
func containsPEMPrivateKey(data []byte) bool {
	for rest := data; ; {
		i := bytes.Index(rest, []byte("-----BEGIN "))
		if i < 0 {
			return false
		}
		rest = rest[i+len("-----BEGIN "):]
		end := bytes.Index(rest, []byte("-----"))
		if end < 0 {
			return false
		}
		if bytes.HasSuffix(rest[:end], []byte("PRIVATE KEY")) {
			return true
		}
	}
}

func resourceFinding(path, detail string, status ReadinessStatus) ReadinessItem {
	return ReadinessItem{ID: "resources:" + path, Label: "Resource " + path, Detail: detail, Status: status, RequiresPerson: status == ReadinessWarn}
}
