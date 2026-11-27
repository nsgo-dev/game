package gamecli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/nsgo-dev/game/pkg/tooling/sizecheck"
)

// sizeCommand measures a built package and checks it against a size budget.
var sizeCommand = &command{
	name:    "size",
	args:    "[package]",
	summary: "Measure a WebAssembly, Android AAB or APK, or iOS IPA package and fail when it exceeds its size budget",
	define:  defineSize,
	complete: map[string]completion{
		"kind":   oneOf("aab", "apk", "ipa", "wasm"),
		"file":   filePaths,
		"budget": filePaths,
		"wasm":   filePaths,
		"":       filePaths,
	},
}

// sizeBytesPerMiB is the number of bytes in the MiB the -budget-mb and -abi-budget-mb flags are given in.
const sizeBytesPerMiB = 1024 * 1024

func defineSize(a *app, fs *flag.FlagSet) func() error {
	kindFlag := fs.String("kind", "", "package `kind`: aab, apk, ipa, or wasm (inferred from the file extension when omitted, defaults to wasm)")
	fileFlag := fs.String("file", "", "path to the package `archive` (.aab, .apk, .ipa, or .wasm); may also be passed as the positional argument")
	budgetPath := fs.String("budget", "", "path to the budget JSON `file` (for wasm defaults to benchmarks/web-budget.json; for mobile packages defaults to the platform's built-in budget)")
	wasmPath := fs.String("wasm", "", "optional path to an already built .wasm `file`; if empty in wasm mode, builds the budget's target with GOOS=js GOARCH=wasm")
	budgetMB := fs.Float64("budget-mb", 0, "override the total package size budget in `MiB` (> 0)")
	abiBudgetMB := fs.Float64("abi-budget-mb", 0, "override the per-ABI native library budget in `MiB` for Android packages (> 0)")
	asJSON := fs.Bool("json", false, "output the report as JSON")
	asMarkdown := fs.Bool("markdown", false, "output the report as a Markdown summary")

	return func() error {
		switch {
		case *asJSON && *asMarkdown:
			return usagef("-json and -markdown are mutually exclusive")
		case *budgetMB < 0 || math.IsNaN(*budgetMB) || math.IsInf(*budgetMB, 0):
			return usagef("-budget-mb must be a finite non-negative number")
		case *abiBudgetMB < 0 || math.IsNaN(*abiBudgetMB) || math.IsInf(*abiBudgetMB, 0):
			return usagef("-abi-budget-mb must be a finite non-negative number")
		}

		pkgPath := *fileFlag
		if fs.NArg() > 1 || (pkgPath != "" && fs.NArg() > 0) {
			return usagef("expected at most one package path")
		}
		if pkgPath == "" && fs.NArg() == 1 {
			pkgPath = fs.Arg(0)
		}

		kind, err := sizeResolveKind(*kindFlag, pkgPath)
		if err != nil {
			return usagef("%v", err)
		}

		if kind == sizecheck.PackageKindWasm {
			return sizeWasm(a, sizeWasmOptions{
				budgetPath: *budgetPath,
				wasmPath:   sizeFirstNonEmpty(*wasmPath, pkgPath),
				budgetMB:   *budgetMB,
				asJSON:     *asJSON,
				asMarkdown: *asMarkdown,
			})
		}

		return sizeMobile(a, sizeMobileOptions{
			kind:        kind,
			pkgPath:     pkgPath,
			budgetPath:  *budgetPath,
			budgetMB:    *budgetMB,
			abiBudgetMB: *abiBudgetMB,
			asJSON:      *asJSON,
			asMarkdown:  *asMarkdown,
		})
	}
}

// sizeResolveKind returns the package kind named by -kind, else the one pkgPath's extension implies, else wasm.
func sizeResolveKind(kindFlag, pkgPath string) (sizecheck.PackageKind, error) {
	if kindFlag != "" {
		return sizecheck.ParsePackageKind(kindFlag)
	}
	if pkgPath != "" {
		if detected, ok := sizecheck.DetectPackageKind(pkgPath); ok {
			return detected, nil
		}
		return "", fmt.Errorf("cannot infer package kind from %q; pass -kind aab|apk|ipa|wasm", pkgPath)
	}
	return sizecheck.PackageKindWasm, nil
}

type sizeWasmOptions struct {
	budgetPath string
	wasmPath   string
	budgetMB   float64
	asJSON     bool
	asMarkdown bool
}

// sizeWasm checks a WebAssembly binary, building the budget's target first when no binary is given.
func sizeWasm(a *app, opts sizeWasmOptions) error {
	bp := opts.budgetPath
	if bp == "" {
		bp = "benchmarks/web-budget.json"
	}

	budget, err := sizecheck.LoadBudget(bp)
	if err != nil {
		return fmt.Errorf("load budget: %w", err)
	}
	if opts.budgetMB > 0 {
		budget.MaxRawBytes = sizeMBToBytes(opts.budgetMB)
	}

	targetFile := opts.wasmPath
	if targetFile == "" {
		if budget.Target == "" {
			return fmt.Errorf("the target in %s is empty and -wasm was not provided", bp)
		}
		tmpDir, err := os.MkdirTemp("", "nsgo-game-wasm-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		targetFile = filepath.Join(tmpDir, "game.wasm")
		buildArgs := []string{"build", "-trimpath", "-tags=release"}
		if budget.LDFlags != "" {
			buildArgs = append(buildArgs, "-ldflags="+budget.LDFlags)
		}
		buildArgs = append(buildArgs, "-o", targetFile, budget.Target)

		cmd := exec.Command("go", buildArgs...)
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
		cmd.Stdout = a.stderr
		cmd.Stderr = a.stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go build wasm failed: %w", err)
		}
	}

	data, err := os.ReadFile(targetFile)
	if err != nil {
		return fmt.Errorf("read wasm: %w", err)
	}

	measured, err := sizecheck.MeasureWasm(data)
	if err != nil {
		return fmt.Errorf("measure wasm: %w", err)
	}

	rep := sizecheck.EvaluateBudget(budget, measured)
	switch {
	case opts.asJSON:
		payload := struct {
			Kind       sizecheck.PackageKind `json:"kind"`
			Budget     sizecheck.BudgetFile  `json:"budget"`
			Measured   sizecheck.Measurement `json:"measured"`
			Violations []string              `json:"violations,omitempty"`
			Passed     bool                  `json:"passed"`
		}{
			Kind:       sizecheck.PackageKindWasm,
			Budget:     rep.Budget,
			Measured:   rep.Measured,
			Violations: rep.Violations,
			Passed:     rep.Passed(),
		}
		if err := sizeWriteJSON(a.stdout, payload); err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
	case opts.asMarkdown:
		_, _ = io.WriteString(a.stdout, sizecheck.FormatWasmMarkdownReport(rep))
	default:
		_, _ = io.WriteString(a.stdout, sizecheck.FormatBudgetReport(rep))
	}

	if !rep.Passed() {
		return fmt.Errorf("%s size budget exceeded (%d violation(s))", sizecheck.PackageKindWasm, len(rep.Violations))
	}
	return nil
}

type sizeMobileOptions struct {
	kind        sizecheck.PackageKind
	pkgPath     string
	budgetPath  string
	budgetMB    float64
	abiBudgetMB float64
	asJSON      bool
	asMarkdown  bool
}

// sizeMobile checks an AAB, APK or IPA archive.
func sizeMobile(a *app, opts sizeMobileOptions) error {
	if opts.pkgPath == "" {
		return usagef("-kind %s requires a package path (via -file or the positional argument)", opts.kind)
	}

	var (
		budget sizecheck.MobileBudget
		err    error
	)
	if opts.budgetPath != "" {
		budget, err = sizecheck.LoadMobileBudget(opts.budgetPath)
		if err != nil {
			return fmt.Errorf("load mobile budget: %w", err)
		}
	} else {
		budget, err = sizecheck.DefaultMobileBudget(opts.kind)
		if err != nil {
			return err
		}
	}

	if opts.budgetMB > 0 {
		budget.MaxTotalBytes = sizeMBToBytes(opts.budgetMB)
	}
	if opts.abiBudgetMB > 0 {
		budget.MaxPerABIBytes = sizeMBToBytes(opts.abiBudgetMB)
	}

	rep, err := sizecheck.CheckMobilePackage(opts.pkgPath, opts.kind, budget)
	var budgetErr *sizecheck.BudgetExceededError
	if err != nil && !errors.As(err, &budgetErr) {
		return fmt.Errorf("inspect %s: %w", opts.kind, err)
	}

	if opts.asJSON {
		if err := sizeWriteJSON(a.stdout, rep); err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
	} else {
		// Both default and -markdown render the structured Markdown report so CI logs and $GITHUB_STEP_SUMMARY get the complete breakdown.
		_, _ = io.WriteString(a.stdout, sizecheck.FormatMobileReport(rep))
	}
	if budgetErr != nil {
		return fmt.Errorf("%s size budget exceeded (%d violation(s))", rep.Kind, len(rep.Violations))
	}
	return nil
}

func sizeMBToBytes(mb float64) int64 {
	return int64(math.Round(mb * sizeBytesPerMiB))
}

func sizeWriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func sizeFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
