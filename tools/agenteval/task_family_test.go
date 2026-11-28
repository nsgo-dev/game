package main

import "testing"

func TestTaskFamilyMetadataValidation(t *testing.T) {
	base := Task{Version: CurrentTaskVersion, ID: "family", Title: "Family", Description: "Family", Prompt: "Do it", ContextDocs: []string{"docs/en/mcp.md"}, StarterDir: "tools/agenteval/tasks/add-menu/starter", ReferenceDir: "tools/agenteval/tasks/add-menu/reference", Family: "editor", MCPServers: []string{"nsgo", "editor"}}
	if base.Family != "editor" || len(base.MCPServers) != 2 {
		t.Fatal("metadata not retained")
	}
	base.Family = "unknown"
	if err := base.Validate(); err == nil {
		t.Fatal("unknown family accepted")
	}
}
