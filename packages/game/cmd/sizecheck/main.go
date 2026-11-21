package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	budgetPath := flag.String("budget", "../../benchmarks/web-budget.json", "path to web size budget JSON")
	wasmPath := flag.String("wasm", "", "optional path to an already-built .wasm file; if empty, builds budget target with GOOS=js GOARCH=wasm")
	flag.Parse()

	budget, err := LoadBudget(*budgetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sizecheck: load budget: %v\n", err)
		os.Exit(2)
	}

	targetFile := *wasmPath
	if targetFile == "" {
		if budget.Target == "" {
			fmt.Fprintln(os.Stderr, "sizecheck: budget target is empty and -wasm was not provided")
			os.Exit(2)
		}
		tmpDir, err := os.MkdirTemp("", "nsgo-game-wasm-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "sizecheck: create temp dir: %v\n", err)
			os.Exit(2)
		}
		defer os.RemoveAll(tmpDir)

		targetFile = filepath.Join(tmpDir, "game.wasm")
		args := []string{"build", "-trimpath"}
		if budget.LDFlags != "" {
			args = append(args, "-ldflags="+budget.LDFlags)
		}
		args = append(args, "-o", targetFile, budget.Target)

		cmd := exec.Command("go", args...)
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "sizecheck: go build wasm failed: %v\n", err)
			os.Exit(2)
		}
	}

	data, err := os.ReadFile(targetFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sizecheck: read wasm: %v\n", err)
		os.Exit(2)
	}

	measured, err := MeasureWasm(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sizecheck: measure wasm: %v\n", err)
		os.Exit(2)
	}

	rep := EvaluateBudget(budget, measured)
	fmt.Print(FormatBudgetReport(rep))
	if !rep.Passed() {
		os.Exit(1)
	}
}
