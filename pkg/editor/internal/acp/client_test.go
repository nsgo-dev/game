package acp

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestCallStreamsNotificationsAndResult(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := New(clientConn, clientConn)
	go func() {
		line, _ := bufio.NewReader(serverConn).ReadString('\n')
		if strings.Contains(line, `"method":"initialize"`) {
			_, _ = fmt.Fprint(serverConn, `{"jsonrpc":"2.0","method":"session/update","params":{"text":"hi"}}
{"jsonrpc":"2.0","id":1,"result":{"ok":true}}
`)
		}
	}()
	var got string
	result, err := client.Initialize(context.Background(), map[string]any{"clientName": "test"}, func(n Notification) error { got = n.Method; return nil })
	if err != nil || got != "session/update" || string(result) != `{"ok":true}` {
		t.Fatalf("result=%s notification=%q err=%v", result, got, err)
	}
}

func TestChoosePermissionFailsClosed(t *testing.T) {
	if got, err := ChoosePermission(PermissionRequest{ToolName: "shell/run", Options: []PermissionOption{{OptionID: "no", Kind: "reject_once"}}}, "editor"); err != nil || got != "no" {
		t.Fatalf("reject = %q, %v", got, err)
	}
	if _, err := ChoosePermission(PermissionRequest{ToolName: "shell/run", Options: []PermissionOption{{OptionID: "yes", Kind: "allow_once"}}}, "editor"); err == nil {
		t.Fatal("unsafe allow succeeded")
	}
	if got, err := ChoosePermission(PermissionRequest{ToolName: "editor/scene_edit", Options: []PermissionOption{{OptionID: "yes", Kind: "allow_once"}}}, "editor"); err != nil || got != "yes" {
		t.Fatalf("editor allow = %q, %v", got, err)
	}
}
