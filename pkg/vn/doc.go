// Package vn presents dialogue scripts as a visual novel: character portraits with expressions standing in stage slots, a text box with name plates and a typewriter, choices, a backlog, auto and skip modes, and voice lines.
//
// A Presenter plays a dialogue.Script (Yarn Spinner) with a Cast read from a YAML cast file. Lines steer the stage with tags (#expr:smile, #at:left, #voice:aiko_001, #offstage) and scripts with the commands <<show>>, <<hide>> and <<expr>>. Update moves it on by a game.Loop's fixed step and Draw draws it into a Canvas, so a scene plays alike every time and under nsgotest.
package vn
