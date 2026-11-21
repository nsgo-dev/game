package main

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
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sizecheck", flag.ContinueOnError)
	fs.SetOutput(stderr)

	kindFlag := fs.String("kind", "", "package kind: aab, apk, ipa, or wasm (inferred from file extension when omitted, defaults to wasm)")
	fileFlag := fs.String("file", "", "path to package archive (.aab, .apk, .ipa, or .wasm); may also be passed as a positional argument")
	budgetPath := fs.String("budget", "", "path to budget JSON (for wasm defaults to ../../benchmarks/web-budget.json; for mobile defaults to platform defaults)")
	wasmPath := fs.String("wasm", "", "optional path to an already-built .wasm file; if empty in wasm mode, builds budget target with GOOS=js GOARCH=wasm")
	budgetMB := fs.Float64("budget-mb", 0, "override total package size budget in MiB (> 0)")
	abiBudgetMB := fs.Float64("abi-budget-mb", 0, "override per-ABI native library budget in MiB for Android packages (> 0)")
	asJSON := fs.Bool("json", false, "output report as JSON")
	asMarkdown := fs.Bool("markdown", false, "output report as Markdown summary")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *asJSON && *asMarkdown {
		fmt.Fprintln(stderr, "sizecheck: -json and -markdown are mutually exclusive")
		return 2
	}
	if *budgetMB < 0 || math.IsNaN(*budgetMB) || math.IsInf(*budgetMB, 0) {
		fmt.Fprintln(stderr, "sizecheck: -budget-mb must be a finite non-negative number")
		return 2
	}
	if *abiBudgetMB < 0 || math.IsNaN(*abiBudgetMB) || math.IsInf(*abiBudgetMB, 0) {
		fmt.Fprintln(stderr, "sizecheck: -abi-budget-mb must be a finite non-negative number")
		return 2
	}

	pkgPath := *fileFlag
	if fs.NArg() > 1 || (pkgPath != "" && fs.NArg() > 0) {
		fmt.Fprintln(stderr, "sizecheck: expected at most one package path")
		return 2
	}
	if pkgPath == "" && fs.NArg() == 1 {
		pkgPath = fs.Arg(0)
	}

	kind, err := resolvePackageKind(*kindFlag, pkgPath, *wasmPath)
	if err != nil {
		fmt.Fprintf(stderr, "sizecheck: %v\n", err)
		return 2
	}

	if kind == PackageKindWasm {
		return runWasm(wasmOptions{
			budgetPath: *budgetPath,
			wasmPath:   firstNonEmpty(*wasmPath, pkgPath),
			budgetMB:   *budgetMB,
			asJSON:     *asJSON,
			asMarkdown: *asMarkdown,
		}, stdout, stderr)
	}

	return runMobile(mobileOptions{
		kind:        kind,
		pkgPath:     pkgPath,
		budgetPath:  *budgetPath,
		budgetMB:    *budgetMB,
		abiBudgetMB: *abiBudgetMB,
		asJSON:      *asJSON,
		asMarkdown:  *asMarkdown,
	}, stdout, stderr)
}

func resolvePackageKind(kindFlag, pkgPath, wasmPath string) (PackageKind, error) {
	if kindFlag != "" {
		return ParsePackageKind(kindFlag)
	}
	if pkgPath != "" {
		if detected, ok := DetectPackageKind(pkgPath); ok {
			return detected, nil
		}
		return "", fmt.Errorf("cannot infer package kind from %q; pass -kind aab|apk|ipa|wasm", pkgPath)
	}
	if wasmPath != "" {
		return PackageKindWasm, nil
	}
	return PackageKindWasm, nil
}

type wasmOptions struct {
	budgetPath string
	wasmPath   string
	budgetMB   float64
	asJSON     bool
	asMarkdown bool
}

func runWasm(opts wasmOptions, stdout, stderr io.Writer) int {
	bp := opts.budgetPath
	if bp == "" {
		bp = "../../benchmarks/web-budget.json"
	}

	budget, err := LoadBudget(bp)
	if err != nil {
		fmt.Fprintf(stderr, "sizecheck: load budget: %v\n", err)
		return 2
	}
	if opts.budgetMB > 0 {
		budget.MaxRawBytes = mbToBytes(opts.budgetMB)
	}

	targetFile := opts.wasmPath
	if targetFile == "" {
		if budget.Target == "" {
			fmt.Fprintln(stderr, "sizecheck: budget target is empty and -wasm was not provided")
			return 2
		}
		tmpDir, err := os.MkdirTemp("", "nsgo-game-wasm-*")
		if err != nil {
			fmt.Fprintf(stderr, "sizecheck: create temp dir: %v\n", err)
			return 2
		}
		defer os.RemoveAll(tmpDir)

		targetFile = filepath.Join(tmpDir, "game.wasm")
		buildArgs := []string{"build", "-trimpath"}
		if budget.LDFlags != "" {
			buildArgs = append(buildArgs, "-ldflags="+budget.LDFlags)
		}
		buildArgs = append(buildArgs, "-o", targetFile, budget.Target)

		cmd := exec.Command("go", buildArgs...)
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
		cmd.Stdout = stderr
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(stderr, "sizecheck: go build wasm failed: %v\n", err)
			return 2
		}
	}

	data, err := os.ReadFile(targetFile)
	if err != nil {
		fmt.Fprintf(stderr, "sizecheck: read wasm: %v\n", err)
		return 2
	}

	measured, err := MeasureWasm(data)
	if err != nil {
		fmt.Fprintf(stderr, "sizecheck: measure wasm: %v\n", err)
		return 2
	}

	rep := EvaluateBudget(budget, measured)
	switch {
	case opts.asJSON:
		payload := struct {
			Kind       PackageKind `json:"kind"`
			Budget     BudgetFile  `json:"budget"`
			Measured   Measurement `json:"measured"`
			Violations []string    `json:"violations,omitempty"`
			Passed     bool        `json:"passed"`
		}{
			Kind:       PackageKindWasm,
			Budget:     rep.Budget,
			Measured:   rep.Measured,
			Violations: rep.Violations,
			Passed:     rep.Passed(),
		}
		if err := writeJSON(stdout, payload); err != nil {
			fmt.Fprintf(stderr, "sizecheck: encode json: %v\n", err)
			return 2
		}
	case opts.asMarkdown:
		_, _ = io.WriteString(stdout, FormatWasmMarkdownReport(rep))
	default:
		_, _ = io.WriteString(stdout, FormatBudgetReport(rep))
	}

	if !rep.Passed() {
		return 1
	}
	return 0
}

type mobileOptions struct {
	kind        PackageKind
	pkgPath     string
	budgetPath  string
	budgetMB    float64
	abiBudgetMB float64
	asJSON      bool
	asMarkdown  bool
}

func runMobile(opts mobileOptions, stdout, stderr io.Writer) int {
	if opts.pkgPath == "" {
		fmt.Fprintf(stderr, "sizecheck: -kind %s requires a package path (via -file or positional argument)\n", opts.kind)
		return 2
	}

	var (
		budget MobileBudget
		err    error
	)
	if opts.budgetPath != "" {
		budget, err = LoadMobileBudget(opts.budgetPath)
		if err != nil {
			fmt.Fprintf(stderr, "sizecheck: load mobile budget: %v\n", err)
			return 2
		}
	} else {
		budget, err = DefaultMobileBudget(opts.kind)
		if err != nil {
			fmt.Fprintf(stderr, "sizecheck: %v\n", err)
			return 2
		}
	}

	if opts.budgetMB > 0 {
		budget.MaxTotalBytes = mbToBytes(opts.budgetMB)
	}
	if opts.abiBudgetMB > 0 {
		budget.MaxPerABIBytes = mbToBytes(opts.abiBudgetMB)
	}

	rep, err := CheckMobilePackage(opts.pkgPath, opts.kind, budget)
	var budgetErr *BudgetExceededError
	if err != nil && !errors.As(err, &budgetErr) {
		fmt.Fprintf(stderr, "sizecheck: inspect %s: %v\n", opts.kind, err)
		return 2
	}

	if opts.asJSON {
		if encErr := writeJSON(stdout, rep); encErr != nil {
			fmt.Fprintf(stderr, "sizecheck: encode json: %v\n", encErr)
			return 2
		}
		if budgetErr != nil {
			fmt.Fprintf(stderr, "sizecheck: %s size budget exceeded (%d violation(s))\n",
				packageKindLabel(rep.Kind), len(rep.Violations))
			return 1
		}
		return 0
	}

	// Both default and -markdown render the structured Markdown report so CI
	// logs and $GITHUB_STEP_SUMMARY get the complete breakdown.
	_, _ = io.WriteString(stdout, FormatMobileReport(rep))
	if budgetErr != nil {
		return 1
	}
	return 0
}

func mbToBytes(mb float64) int64 {
	return int64(math.Round(mb * float64(bytesPerMiB)))
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
