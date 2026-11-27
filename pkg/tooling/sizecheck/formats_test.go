package sizecheck

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
)

// TestBudgetExamplesOnFormatsPage loads each example of the two budget entries on the File Formats page with LoadBudget and LoadMobileBudget, and decodes it strictly too, since those readers skip a key they do not know.
func TestBudgetExamplesOnFormatsPage(t *testing.T) {
	strict := func(data []byte, v any) error {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		return dec.Decode(v)
	}
	for _, ex := range formattest.Examples(t, "tooling/sizecheck", "json") {
		file := filepath.Join(t.TempDir(), "budget.json")
		if err := os.WriteFile(file, ex.Data, 0o644); err != nil {
			t.Fatal(err)
		}
		b, err := LoadBudget(file)
		if err == nil {
			err = strict(ex.Data, &BudgetFile{})
		}
		if err != nil || b.Target == "" || b.MaxRawBytes <= 0 {
			t.Errorf("%s:%d: the example does not load as a WebAssembly budget with a target and a raw limit: %+v, %v", ex.Page, ex.Line, b, err)
		}
	}
	for _, ex := range formattest.Examples(t, "tooling/sizecheck#mobile", "json") {
		file := filepath.Join(t.TempDir(), "budget.json")
		if err := os.WriteFile(file, ex.Data, 0o644); err != nil {
			t.Fatal(err)
		}
		b, err := LoadMobileBudget(file)
		if err == nil {
			err = strict(ex.Data, &MobileBudget{})
		}
		if err != nil || b.MaxTotalBytes <= 0 {
			t.Errorf("%s:%d: the example does not load as a mobile budget with a total limit: %+v, %v", ex.Page, ex.Line, b, err)
		}
	}
}
