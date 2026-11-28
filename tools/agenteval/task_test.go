package main

import "testing"

func TestTaskFamilyAndMCPServerMetadata(t *testing.T) {
	tasks, err := LoadTasksDir("tasks")
	if err != nil {
		t.Fatal(err)
	}
	var task *Task
	for _, candidate := range tasks {
		if candidate.ID == "editor-workflow" {
			task = candidate
		}
	}
	if task == nil {
		t.Fatal("editor-workflow task not found")
	}
	if err != nil {
		t.Fatal(err)
	}
	if task.Family != "editor" || len(task.MCPServers) != 1 || task.MCPServers[0] != "nsgo" {
		t.Fatalf("metadata=%+v", task)
	}
	bad := *task
	bad.Family = "unknown"
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown family accepted")
	}
	bad = *task
	bad.MCPServers = []string{"nsgo", "nsgo"}
	if err := bad.Validate(); err == nil {
		t.Fatal("duplicate server accepted")
	}
}
