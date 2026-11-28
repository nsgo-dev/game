// Package acp implements the JSON-RPC-over-stdio core of the Agent Client
// Protocol. It deliberately exposes protocol values without depending on the
// editor, so probes and runners can share one transport implementation.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

type Client struct {
	r    *bufio.Reader
	w    io.Writer
	mu   sync.Mutex
	next uint64
}

func New(r io.Reader, w io.Writer) *Client { return &Client{r: bufio.NewReader(r), w: w, next: 1} }

type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error,omitempty"`
}
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Call sends one request and reads until its response. Notifications received
// before the response are passed to onNotification in arrival order.
func (c *Client) Call(ctx context.Context, method string, params any, onNotification func(Notification) error) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.r == nil || c.w == nil {
		return nil, errors.New("acp: nil transport")
	}
	id := c.next
	c.next++
	b, err := json.Marshal(Request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("acp: encode %s: %w", method, err)
	}
	if _, err := c.w.Write(append(b, '\n')); err != nil {
		return nil, fmt.Errorf("acp: write %s: %w", method, err)
	}
	for {
		line, err := readContext(ctx, c.r)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			ID     *uint64         `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			return nil, fmt.Errorf("acp: decode response: %w", err)
		}
		if envelope.Method != "" && envelope.ID == nil {
			if onNotification != nil {
				if err := onNotification(Notification{JSONRPC: "2.0", Method: envelope.Method, Params: envelope.Params}); err != nil {
					return nil, err
				}
			}
			continue
		}
		if envelope.ID == nil || *envelope.ID != id {
			continue
		}
		var response Response
		if err := json.Unmarshal(line, &response); err != nil {
			return nil, fmt.Errorf("acp: decode response: %w", err)
		}
		if response.Error != nil {
			return nil, fmt.Errorf("acp: %s (%d)", response.Error.Message, response.Error.Code)
		}
		return response.Result, nil
	}
}

func readContext(ctx context.Context, r *bufio.Reader) ([]byte, error) {
	ch := make(chan struct {
		b   []byte
		err error
	}, 1)
	go func() {
		b, err := r.ReadBytes('\n')
		ch <- struct {
			b   []byte
			err error
		}{b, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		if result.err != nil {
			return nil, fmt.Errorf("acp: read: %w", result.err)
		}
		return []byte(strings.TrimSpace(string(result.b))), nil
	}
}

func (c *Client) Notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{"2.0", method, params})
	if err != nil {
		return err
	}
	_, err = c.w.Write(append(b, '\n'))
	return err
}

func (c *Client) Initialize(ctx context.Context, params any, on func(Notification) error) (json.RawMessage, error) {
	return c.Call(ctx, "initialize", params, on)
}
func (c *Client) SessionNew(ctx context.Context, params any, on func(Notification) error) (json.RawMessage, error) {
	return c.Call(ctx, "session/new", params, on)
}
func (c *Client) SessionResume(ctx context.Context, params any, on func(Notification) error) (json.RawMessage, error) {
	return c.Call(ctx, "session/resume", params, on)
}
func (c *Client) SessionSetConfigOption(ctx context.Context, params any, on func(Notification) error) (json.RawMessage, error) {
	return c.Call(ctx, "session/set_config_option", params, on)
}
func (c *Client) Prompt(ctx context.Context, params any, on func(Notification) error) (json.RawMessage, error) {
	return c.Call(ctx, "session/prompt", params, on)
}
func (c *Client) Cancel(ctx context.Context, params any) error {
	return c.Notify("session/cancel", params)
}

type PermissionOption struct {
	OptionID string `json:"optionId"`
	Kind     string `json:"kind"`
	ToolName string `json:"toolName,omitempty"`
}
type PermissionRequest struct {
	ToolName string             `json:"toolName"`
	Options  []PermissionOption `json:"options"`
}

// ChoosePermission enforces editor policy for an ACP permission request. Only
// an editor-owned MCP tool may be allowed once. All other requests must offer
// reject_once, and requests without that option fail closed.
func ChoosePermission(req PermissionRequest, editorServer string) (string, error) {
	for _, option := range req.Options {
		if option.Kind == "allow_once" && editorServer != "" && strings.HasPrefix(req.ToolName, editorServer+"/") {
			return option.OptionID, nil
		}
	}
	for _, option := range req.Options {
		if option.Kind == "reject_once" {
			return option.OptionID, nil
		}
	}
	return "", fmt.Errorf("acp: permission request for %q has no safe option", req.ToolName)
}
