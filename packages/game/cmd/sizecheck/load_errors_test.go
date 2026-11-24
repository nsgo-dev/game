package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
)

func TestBudgetLoadersNameFileLineAndFix(t *testing.T) {
	dir := t.TempDir()
	web := filepath.Join(dir, "budget.json")
	if err := os.WriteFile(web, []byte("{\n  \"target\": \"wasm\",\n  \"max_raw_bytes\": \"8MB\"\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadBudget(web)
	loaderrtest.Expect(t, err, web, 3)

	mobile := filepath.Join(dir, "mobile.json")
	if err := os.WriteFile(mobile, []byte("{\n  \"max_total_bytes\": 1,\n  \"max_per_abi_bytes\": true\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = LoadMobileBudget(mobile)
	loaderrtest.Expect(t, err, mobile, 3)

	missing := filepath.Join(dir, "none.json")
	_, err = LoadBudget(missing)
	loaderrtest.Expect(t, err, missing, 0)
}
