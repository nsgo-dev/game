package credits

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
)

const maxSourceSidecarBytes = 1 << 20 // 1 MiB

// GeneratedAssetLicense summarizes the provenance and license recorded in one generated asset's *.source.yaml sidecar.
type GeneratedAssetLicense struct {
	// Asset is the generated asset's slash-separated path relative to the project root.
	Asset string
	// SourcePath is the slash-separated path of the asset's *.source.yaml sidecar relative to the project root.
	SourcePath string
	// Generator is the MCP generator server name recorded in the sidecar.
	Generator string
	// Model is the model identifier recorded in the sidecar, if any.
	Model string
	// License is the license string recorded in the sidecar (possibly empty or "unknown").
	License string
	// CommercialUse reports whether the sidecar allows commercial use.
	CommercialUse bool
	// ApprovedBy names the person who approved the pick, if recorded.
	ApprovedBy string
	// UnknownLicense reports whether License is empty or unknown and therefore blocks release builds.
	UnknownLicense bool
}

// UnknownLicenseError is returned by CheckGeneratedLicenses and Collect when one or more generated assets (*.source.yaml) have an empty or unknown license.
type UnknownLicenseError struct {
	// Assets are the slash-separated paths of generated assets whose license is unknown, sorted.
	Assets []string
}

// Error names every generated asset whose unknown license blocks a release build.
func (e *UnknownLicenseError) Error() string {
	var b strings.Builder
	n := len(e.Assets)
	if n == 1 {
		b.WriteString("credits: 1 generated asset has an unknown license; ")
	} else {
		fmt.Fprintf(&b, "credits: %d generated assets have unknown licenses; ", n)
	}
	b.WriteString("set a license in each generated asset's .source.yaml sidecar before releasing:")
	for _, a := range e.Assets {
		b.WriteString("\n\t")
		b.WriteString(a)
		b.WriteString(" (")
		b.WriteString(importsettings.AssetSourcePath(a))
		b.WriteString(")")
	}
	return b.String()
}

// ScanGeneratedLicenses walks root and reads every generated asset's *.source.yaml sidecar in deterministic path order, skipping dot-prefixed directories, node_modules, and vendor.
// A malformed *.source.yaml returns an error naming its relative path.
func ScanGeneratedLicenses(root string) ([]GeneratedAssetLicense, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("credits: %w", err)
	}
	if !info.IsDir() {
		return nil, nil
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("credits: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		rootReal = rootAbs
	}
	var out []GeneratedAssetLicense
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if p != root {
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !importsettings.IsAssetSource(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("credits: %s: symlink source sidecar is not allowed", relSlash)
		}
		targetReal, err := filepath.EvalSymlinks(p)
		if err != nil {
			return fmt.Errorf("credits: %s: %w", relSlash, err)
		}
		relReal, err := filepath.Rel(rootReal, targetReal)
		if err != nil || relReal == ".." || strings.HasPrefix(relReal, ".."+string(filepath.Separator)) || filepath.IsAbs(relReal) {
			return fmt.Errorf("credits: %s: resolves outside project root", relSlash)
		}
		data, err := readBoundedFile(p, maxSourceSidecarBytes)
		if err != nil {
			return fmt.Errorf("credits: %s: %w", relSlash, err)
		}
		src, err := importsettings.ParseAssetSource(data)
		if err != nil {
			return fmt.Errorf("credits: %s: %w", relSlash, err)
		}
		assetPath := strings.TrimSuffix(relSlash, importsettings.AssetSourceSuffix)
		out = append(out, GeneratedAssetLicense{
			Asset:          assetPath,
			SourcePath:     relSlash,
			Generator:      src.Generator,
			Model:          src.Model,
			License:        src.License,
			CommercialUse:  src.CommercialUse,
			ApprovedBy:     src.ApprovedBy,
			UnknownLicense: importsettings.IsUnknownLicense(src.License),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b GeneratedAssetLicense) int {
		return strings.Compare(a.Asset, b.Asset)
	})
	return out, nil
}

// CheckGeneratedLicenses scans every *.source.yaml sidecar under root and fails with *UnknownLicenseError when any generated asset has an empty or unknown license, naming every blocked asset path.
func CheckGeneratedLicenses(root string) ([]GeneratedAssetLicense, error) {
	items, err := ScanGeneratedLicenses(root)
	if err != nil {
		return nil, err
	}
	var unknown []string
	for _, it := range items {
		if it.UnknownLicense {
			unknown = append(unknown, it.Asset)
		}
	}
	if len(unknown) > 0 {
		return items, &UnknownLicenseError{Assets: unknown}
	}
	return items, nil
}

func readBoundedFile(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds maximum size of %d bytes", maxBytes)
	}
	return data, nil
}
