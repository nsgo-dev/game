package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/game/pkg/mcp"
)

func TestVarsTools(t *testing.T) {
	doc, err := flow.ParseVars([]byte(`version: 1
vars:
  - name: bridge_repaired
    kind: bool
    doc: East bridge switch.
  - name: gold
    kind: int
    default: 50
    min: 0
    max: 500
    doc: Player gold.
  - name: affinity
    kind: float
    default: 0.5
    min: 0
    max: 1
  - name: hero_name
    kind: string
    default: Aria
`))
	if err != nil {
		t.Fatal(err)
	}
	store, err := flow.NewVars(doc)
	if err != nil {
		t.Fatal(err)
	}
	store.Self("chest_01").SetBool("opened", true)

	bridge := mcp.New(mcp.Config{Vars: store})
	tools := make(map[string]func(context.Context, json.RawMessage) (any, error))
	for _, tool := range bridge.Tools() {
		tools[tool.Name] = tool.Handler
	}

	// List all vars and self vars.
	rawList, err := tools[mcp.ToolVarsList](context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("game_vars_list: %v", err)
	}
	listRes := rawList.(mcp.VarsListResult)
	if listRes.Total != 4 || len(listRes.Vars) != 4 {
		t.Fatalf("listRes = %+v, want 4 vars", listRes)
	}
	if listRes.Self["chest_01"]["opened"] != true {
		t.Fatalf("listRes.Self = %+v, want chest_01.opened=true", listRes.Self)
	}

	// Prefix filter.
	rawFiltered, err := tools[mcp.ToolVarsList](context.Background(), json.RawMessage(`{"prefix":"bridge","include_self":false}`))
	if err != nil {
		t.Fatalf("filtered list: %v", err)
	}
	filtered := rawFiltered.(mcp.VarsListResult)
	if filtered.Total != 1 || filtered.Vars[0].Name != "bridge_repaired" || len(filtered.Self) != 0 {
		t.Fatalf("filtered = %+v", filtered)
	}

	// Get single var.
	rawGet, err := tools[mcp.ToolVarGet](context.Background(), json.RawMessage(`{"name":"$gold"}`))
	if err != nil {
		t.Fatalf("game_var_get: %v", err)
	}
	getRes := rawGet.(mcp.VarResult)
	if getRes.Name != "gold" || getRes.Value != 50 || getRes.Kind != "int" {
		t.Fatalf("getRes = %+v", getRes)
	}

	// Set valid values.
	rawSet, err := tools[mcp.ToolVarSet](context.Background(), json.RawMessage(`{"name":"bridge_repaired","value":true}`))
	if err != nil {
		t.Fatalf("set bool: %v", err)
	}
	setRes := rawSet.(mcp.VarSetResult)
	if setRes.OldValue != false || setRes.Value != true {
		t.Fatalf("setRes = %+v", setRes)
	}
	if b, _ := store.Bool("bridge_repaired"); !b {
		t.Fatalf("store bridge_repaired not updated")
	}

	// Set out-of-range refused without clamp, clamped with clamp:true.
	if _, err := tools[mcp.ToolVarSet](context.Background(), json.RawMessage(`{"name":"gold","value":999}`)); err == nil {
		t.Fatal("expected out-of-range error without clamp")
	}
	rawClamped, err := tools[mcp.ToolVarSet](context.Background(), json.RawMessage(`{"name":"gold","value":999,"clamp":true}`))
	if err != nil {
		t.Fatalf("set clamped: %v", err)
	}
	clampedRes := rawClamped.(mcp.VarSetResult)
	if !clampedRes.Clamped || clampedRes.Value != 500 {
		t.Fatalf("clampedRes = %+v", clampedRes)
	}

	// Wrong type refused.
	if _, err := tools[mcp.ToolVarSet](context.Background(), json.RawMessage(`{"name":"gold","value":"lots"}`)); err == nil || !strings.Contains(err.Error(), "expects int") {
		t.Fatalf("wrong type err = %v", err)
	}

	// Unknown var refused.
	if _, err := tools[mcp.ToolVarGet](context.Background(), json.RawMessage(`{"name":"missing"}`)); err == nil {
		t.Fatal("expected unknown var error")
	}
}
