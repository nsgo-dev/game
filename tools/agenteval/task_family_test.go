package main

import (
	"strings"
	"testing"
)

func TestTaskFamilyMetadataValidation(t *testing.T) {
	root := testRepoRoot(t)
	for _, id := range []string{"platformer-level", "add-enemy-type", "add-menu"} {
		task := loadTask(t, root, id)
		if task.Family != "create" || len(task.MCPServers) != 1 || task.MCPServers[0] != "nsgo" {
			t.Fatalf("task %s: family %q, mcp_servers %q; want create, [nsgo]", id, task.Family, task.MCPServers)
		}
	}

	base := loadTask(t, root, "add-menu")
	cases := []struct {
		name    string
		mutate  func(*Task)
		wantErr string
	}{
		{"family omitted", func(task *Task) { task.Family = ""; task.MCPServers = nil }, ""},
		{"editor family", func(task *Task) { task.Family = "editor"; task.MCPServers = []string{"nsgo", "editor"} }, ""},
		{"unknown family", func(task *Task) { task.Family = "unknown" }, `unknown family "unknown"`},
		{"blank server", func(task *Task) { task.MCPServers = []string{" "} }, "invalid mcp_servers entry"},
		{"server with space", func(task *Task) { task.MCPServers = []string{"game editor"} }, "invalid mcp_servers entry"},
		{"duplicate server", func(task *Task) { task.MCPServers = []string{"nsgo", "nsgo"} }, `mcp_servers contains duplicate "nsgo"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := *base
			task.MCPServers = append([]string(nil), base.MCPServers...)
			tc.mutate(&task)
			err := task.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
