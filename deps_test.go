package game_test

import (
	"os/exec"
	"strings"
	"testing"
)

// headlessPackages are the importable packages a headless build, such as a
// dedicated server (docs/en/server.md), links. None of them may depend on
// NSGo, the platform and windowing layer. Add a package here when another
// package gets the same headless guarantee.
var headlessPackages = []string{
	"./pkg/online/server/...",
	"./pkg/datatable",
	"./pkg/jsonschema",
}

// TestHeadlessPackagesDoNotDependOnNSGo fails when a headless package pulls
// NSGo back in, for example through an import of tooling/hotreload.
func TestHeadlessPackagesDoNotDependOnNSGo(t *testing.T) {
	for _, pattern := range headlessPackages {
		out, err := exec.Command("go", "list", "-deps", pattern).CombinedOutput()
		if err != nil {
			t.Fatalf("go list -deps %s: %v\n%s", pattern, err, out)
		}
		var deps []string
		for line := range strings.Lines(string(out)) {
			line = strings.TrimSpace(line)
			if line == "github.com/nsgo-dev/nsgo" || strings.HasPrefix(line, "github.com/nsgo-dev/nsgo/") {
				deps = append(deps, line)
			}
		}
		if len(deps) > 0 {
			t.Errorf("%s must not depend on NSGo, but pulls in: %s", pattern, strings.Join(deps, ", "))
		}
	}
}
