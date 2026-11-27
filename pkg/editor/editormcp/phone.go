package editormcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
)

const (
	// ToolPhoneSessionsList lists phone/browser Play-mode web build sessions connected
	// to the editor's LAN QR MCP bridge.
	ToolPhoneSessionsList = "phone_sessions_list"
	// ToolPhoneCallTool forwards a game MCP tool call to a connected phone/browser Play-mode
	// session through Editor.CallWebBridgeTool under the editor's permission rules.
	ToolPhoneCallTool = "phone_call_tool"
)

const (
	defaultPhoneCallTimeout = 15 * time.Second
	minPhoneCallTimeoutMS   = 100
	maxPhoneCallTimeoutMS   = 60_000
)

// PhoneSessionsListResult is the structured result of phone_sessions_list.
type PhoneSessionsListResult struct {
	Serving       bool                      `json:"serving"`
	URL           string                    `json:"url,omitempty"`
	BridgeInfoURL string                    `json:"bridge_info_url,omitempty"`
	SessionCount  int                       `json:"session_count"`
	Sessions      []editor.WebBridgeSession `json:"sessions"`
}

// PhoneCallToolResult is the structured result of phone_call_tool.
type PhoneCallToolResult struct {
	SessionID string          `json:"session_id"`
	Device    string          `json:"device,omitempty"`
	Tool      string          `json:"tool"`
	Result    json.RawMessage `json:"result"`
}

func phoneTools() []Tool {
	return []Tool{
		{
			Name:        ToolPhoneSessionsList,
			Title:       "List connected phone sessions",
			Description: "List Play-mode web build sessions connected to the editor's LAN QR MCP bridge, including the LAN server status, URL, and each connected phone/browser session's ID, device name, IP, capabilities, and connection time.",
			InputSchema: []byte(`{"type":"object","additionalProperties":false,"properties":{}}`),
			Action:      ActionRead,
			Handler:     phoneSessionsListTool,
		},
		{
			Name:        ToolPhoneCallTool,
			Title:       "Call a tool on a connected phone",
			Description: "Forward a game MCP tool call (such as game_state_dump, game_scenes_list, game_entities_list, game_entity_inspect, game_loop_status, game_loop_pause, game_loop_step, game_input_action, game_console_exec, game_screenshot, or tools/list) to a Play-mode web build connected over the editor's LAN MCP bridge. When session_id is omitted and exactly one phone session is connected, that session is selected automatically.",
			InputSchema: []byte(`{"type":"object","additionalProperties":false,"required":["tool"],"properties":{"session_id":{"type":"string","description":"Target phone session ID from phone_sessions_list; optional when exactly one session is connected"},"tool":{"type":"string","description":"Game MCP tool name to invoke on the phone build (for example game_state_dump, game_scenes_list, game_entities_list, game_screenshot, game_loop_status, game_input_action)"},"arguments":{"type":"object","description":"JSON object of arguments forwarded to the game MCP tool (empty object when omitted)"},"timeout_ms":{"type":"integer","minimum":100,"maximum":60000,"description":"Optional timeout in milliseconds waiting for the phone to reply (default 15000)"}}}`),
			Action:      ActionRun,
			Target:      phoneCallTarget,
			Handler:     phoneCallTool,
		},
	}
}

func phoneCallTarget(args json.RawMessage, _ *editor.Editor) (editor.PermissionAction, string) {
	var in struct {
		Tool string `json:"tool"`
	}
	_ = json.Unmarshal(args, &in)
	toolName := strings.TrimSpace(in.Tool)
	if toolName == "" {
		return editor.PermissionActionRun, editor.PermissionRunPlaytests + ": phone"
	}
	return editor.PermissionActionRun, editor.PermissionRunPlaytests + ": phone/" + toolName
}

func phoneSessionsListTool(ctx context.Context, c *Call) (*Result, error) {
	var args struct{}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("%s: %w", ToolPhoneSessionsList, err)
	}
	var snap editor.WebBuildServerSnapshot
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		snap = ed.WebBuildServerState()
		return nil
	}); err != nil {
		return nil, err
	}
	sessions := snap.ConnectedClients
	if sessions == nil {
		sessions = []editor.WebBridgeSession{}
	}
	var b strings.Builder
	switch {
	case !snap.Serving:
		b.WriteString("No phone sessions connected (LAN web build server is not running).")
	case len(sessions) == 0:
		fmt.Fprintf(&b, "No phone sessions connected (serving %s).", snap.URL)
	default:
		fmt.Fprintf(&b, "%d phone session(s) connected to %s:", len(sessions), snap.URL)
		for _, s := range sessions {
			fmt.Fprintf(&b, "\n- %s (%s, ip=%s", s.SessionID, s.Device, s.RemoteIP)
			if len(s.Capabilities) > 0 {
				fmt.Fprintf(&b, ", capabilities=%s", strings.Join(s.Capabilities, ","))
			}
			b.WriteByte(')')
		}
	}
	return &Result{
		Text: b.String(),
		Structured: PhoneSessionsListResult{
			Serving:       snap.Serving,
			URL:           snap.URL,
			BridgeInfoURL: snap.BridgeInfoURL,
			SessionCount:  len(sessions),
			Sessions:      sessions,
		},
	}, nil
}

func phoneCallTool(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		SessionID string          `json:"session_id"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
		TimeoutMS int             `json:"timeout_ms"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("%s: %w", ToolPhoneCallTool, err)
	}
	toolName := strings.TrimSpace(args.Tool)
	if toolName == "" {
		return nil, fmt.Errorf("%s: tool is required", ToolPhoneCallTool)
	}
	if args.TimeoutMS != 0 && (args.TimeoutMS < minPhoneCallTimeoutMS || args.TimeoutMS > maxPhoneCallTimeoutMS) {
		return nil, fmt.Errorf("%s: timeout_ms must be between %d and %d", ToolPhoneCallTool, minPhoneCallTimeoutMS, maxPhoneCallTimeoutMS)
	}
	rawArgs := bytes.TrimSpace(args.Arguments)
	if len(rawArgs) == 0 || bytes.Equal(rawArgs, []byte("null")) {
		rawArgs = json.RawMessage(`{}`)
	} else {
		var obj map[string]any
		if err := json.Unmarshal(rawArgs, &obj); err != nil {
			return nil, fmt.Errorf("%s: arguments must be a JSON object", ToolPhoneCallTool)
		}
	}

	var (
		edRef     *editor.Editor
		sessionID = strings.TrimSpace(args.SessionID)
		device    string
	)
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		edRef = ed
		snap := ed.WebBuildServerState()
		if !snap.Serving {
			return fmt.Errorf("%s: LAN web build server is not running; build and serve a Web build first", ToolPhoneCallTool)
		}
		if sessionID == "" {
			switch len(snap.ConnectedClients) {
			case 0:
				return fmt.Errorf("%s: no phone sessions connected to %s; scan the LAN QR code on a phone first", ToolPhoneCallTool, snap.URL)
			case 1:
				sessionID = snap.ConnectedClients[0].SessionID
				device = snap.ConnectedClients[0].Device
			default:
				ids := make([]string, 0, len(snap.ConnectedClients))
				for _, s := range snap.ConnectedClients {
					ids = append(ids, s.SessionID)
				}
				return fmt.Errorf("%s: %d phone sessions connected (%s); pass session_id to choose one", ToolPhoneCallTool, len(ids), strings.Join(ids, ", "))
			}
			return nil
		}
		for _, s := range snap.ConnectedClients {
			if s.SessionID == sessionID {
				device = s.Device
				return nil
			}
		}
		return fmt.Errorf("%s: %w: %q", ToolPhoneCallTool, editor.ErrWebBridgeSessionNotFound, sessionID)
	}); err != nil {
		return nil, err
	}

	timeout := defaultPhoneCallTimeout
	if args.TimeoutMS > 0 {
		timeout = time.Duration(args.TimeoutMS) * time.Millisecond
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Dispatch outside Call.Editor so waiting on the phone over LAN never blocks the editor's UI goroutine.
	rawResult, err := edRef.CallWebBridgeTool(callCtx, sessionID, toolName, json.RawMessage(rawArgs))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%s: phone session %q did not reply to %s within %s", ToolPhoneCallTool, sessionID, toolName, timeout)
		}
		return nil, fmt.Errorf("%s: %s on %s: %w", ToolPhoneCallTool, toolName, sessionID, err)
	}
	if len(bytes.TrimSpace(rawResult)) == 0 {
		rawResult = json.RawMessage(`null`)
	}

	var pngBytes []byte
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(rawResult, &obj); err == nil {
		if b64Raw, ok := obj["png_base64"]; ok {
			var b64 string
			if err := json.Unmarshal(b64Raw, &b64); err == nil && strings.TrimSpace(b64) != "" {
				if decoded, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(b64)); derr == nil && len(decoded) > 0 {
					pngBytes = decoded
				}
			}
		}
	}

	var textBuf bytes.Buffer
	if err := json.Indent(&textBuf, rawResult, "", "  "); err != nil {
		textBuf.Reset()
		textBuf.Write(rawResult)
	}
	return &Result{
		Text: textBuf.String(),
		Structured: PhoneCallToolResult{
			SessionID: sessionID,
			Device:    device,
			Tool:      toolName,
			Result:    rawResult,
		},
		PNG: pngBytes,
	}, nil
}
