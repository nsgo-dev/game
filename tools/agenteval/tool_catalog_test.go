package main

import "testing"

func TestRequiredToolsRejectUnknownAndDuplicate(t *testing.T) {
	if err := validateRequiredTools("x", []string{"scene_edit", "game_check"}); err != nil {
		t.Fatal(err)
	}
	if err := validateRequiredTools("x", []string{"unknown"}); err == nil {
		t.Fatal("unknown tool accepted")
	}
	if err := validateRequiredTools("x", []string{"scene_edit", "scene_edit"}); err == nil {
		t.Fatal("duplicate tool accepted")
	}
}
