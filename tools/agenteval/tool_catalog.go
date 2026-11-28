package main

import "fmt"

var knownAgentTools = map[string]bool{"game_scenes_list": true, "game_layers_list": true, "game_entities_list": true, "game_entity_inspect": true, "game_input_action": true, "game_input_pointer": true, "scene_edit": true, "file_edit": true, "datatable_review_propose": true, "game_check": true, "game_replay_list": true, "game_replay_play": true}

func validateRequiredTools(taskID string, tools []string) error {
	seen := map[string]bool{}
	for _, name := range tools {
		if seen[name] {
			return fmt.Errorf("task %q: required_tools contains duplicate %q", taskID, name)
		}
		if !knownAgentTools[name] {
			return fmt.Errorf("task %q: unknown required tool %q", taskID, name)
		}
		seen[name] = true
	}
	return nil
}
