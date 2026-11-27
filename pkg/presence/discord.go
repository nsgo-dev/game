package presence

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DiscordMaxTextLength is the maximum UTF-8 byte length Discord accepts for
// rich presence text fields such as details, state, and asset hover text.
const DiscordMaxTextLength = 128

const (
	discordIPCOpHandshake uint32 = 0
	discordIPCOpFrame     uint32 = 1
	discordIPCOpClose     uint32 = 2
	discordIPCOpPing      uint32 = 3
	discordIPCOpPong      uint32 = 4

	maxDiscordFrameBytes = 1 << 20 // 1 MiB safety cap on inbound IPC frames
)

// DiscordConfig configures a [Discord] local IPC rich presence provider.
type DiscordConfig struct {
	// ClientID is the Discord Application ID from the Discord Developer Portal.
	// Required.
	ClientID string

	// PID is the process ID reported in SET_ACTIVITY commands. When zero or
	// negative, os.Getpid() is used.
	PID int

	// Dial opens the local Discord IPC transport. When nil, the platform
	// default is used (Unix domain sockets discord-ipc-0..9 on macOS and Linux,
	// named pipes \\.\pipe\discord-ipc-0..9 on Windows, and ErrUnavailable on
	// mobile and web targets).
	Dial func(ctx context.Context) (io.ReadWriteCloser, error)
}

// Discord publishes rich presence updates to a locally running Discord desktop
// client using Discord's local IPC v1 protocol in pure Go (no cgo).
//
// Connections are established lazily on the first Set or Clear call (or eagerly
// via [Discord.Connect]). If Discord is not running or the current target has
// no desktop IPC transport, Set and Clear return [ErrUnavailable]. If an active
// IPC connection drops mid-session, Discord automatically attempts a single
// reconnect and handshake before returning an error.
//
// All methods on Discord are safe for concurrent use.
type Discord struct {
	mu       sync.Mutex
	clientID string
	pid      int
	dial     func(ctx context.Context) (io.ReadWriteCloser, error)

	conn   io.ReadWriteCloser
	nonce  uint64
	closed bool
}

// NewDiscord constructs a Discord rich presence provider. It returns
// [ErrInvalidConfig] if cfg.ClientID is empty or whitespace.
func NewDiscord(cfg DiscordConfig) (*Discord, error) {
	clientID := strings.TrimSpace(cfg.ClientID)
	if clientID == "" {
		return nil, fmt.Errorf("%w: discord ClientID is required", ErrInvalidConfig)
	}
	pid := cfg.PID
	if pid <= 0 {
		pid = os.Getpid()
	}
	dialFn := cfg.Dial
	if dialFn == nil {
		dialFn = dialDiscordIPC
	}
	return &Discord{
		clientID: clientID,
		pid:      pid,
		dial:     dialFn,
	}, nil
}

// Name returns "discord".
func (d *Discord) Name() string { return "discord" }

// Connect eagerly opens the local Discord IPC connection and performs the v1
// handshake if not already connected.
func (d *Discord) Connect(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrClosed
	}
	return d.ensureConnectedLocked(ctx)
}

// Set publishes activity to the local Discord client via SET_ACTIVITY.
func (d *Discord) Set(ctx context.Context, activity Activity) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	payload := buildDiscordActivity(activity)
	return d.sendActivity(ctx, payload)
}

// Clear removes the current Discord rich presence by sending SET_ACTIVITY with
// activity set to null.
func (d *Discord) Clear(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.sendActivity(ctx, nil)
}

// Close closes the Discord IPC connection and marks the provider closed.
func (d *Discord) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	conn := d.conn
	d.conn = nil
	d.mu.Unlock()

	if conn == nil {
		return nil
	}
	// Best-effort close notification before tearing down the socket/pipe.
	_ = writeDiscordFrame(conn, discordIPCOpClose, []byte("{}"))
	return conn.Close()
}

func (d *Discord) sendActivity(ctx context.Context, act *discordActivity) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return ErrClosed
	}

	wasConnected := d.conn != nil
	if err := d.ensureConnectedLocked(ctx); err != nil {
		return err
	}

	err := d.doSetActivityLocked(ctx, act)
	if err == nil {
		return nil
	}
	// If we reused an existing connection and hit a transport failure (not a
	// protocol-level Discord error or context cancellation), reconnect once.
	var protoErr *discordProtocolError
	if !wasConnected || errors.As(err, &protoErr) || ctx.Err() != nil {
		if !errors.As(err, &protoErr) {
			d.dropConnLocked()
		}
		return err
	}

	d.dropConnLocked()
	if err := d.ensureConnectedLocked(ctx); err != nil {
		return err
	}
	if err := d.doSetActivityLocked(ctx, act); err != nil {
		if !errors.As(err, &protoErr) {
			d.dropConnLocked()
		}
		return err
	}
	return nil
}

func (d *Discord) ensureConnectedLocked(ctx context.Context) error {
	if d.conn != nil {
		return nil
	}
	conn, err := d.dial(ctx)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if err := d.handshakeLocked(ctx, conn); err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	d.conn = conn
	return nil
}

func (d *Discord) dropConnLocked() {
	if d.conn != nil {
		_ = d.conn.Close()
		d.conn = nil
	}
}

func (d *Discord) handshakeLocked(ctx context.Context, conn io.ReadWriteCloser) error {
	req, err := json.Marshal(discordHandshake{
		Version:  1,
		ClientID: d.clientID,
	})
	if err != nil {
		return err
	}

	stopWatch := watchContextConn(ctx, conn)
	defer stopWatch()

	if err := writeDiscordFrame(conn, discordIPCOpHandshake, req); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("discord handshake write: %w", err)
	}

	for {
		op, payload, err := readDiscordFrame(conn)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("discord handshake read: %w", err)
		}
		switch op {
		case discordIPCOpPing:
			if err := writeDiscordFrame(conn, discordIPCOpPong, payload); err != nil {
				return fmt.Errorf("discord pong: %w", err)
			}
		case discordIPCOpClose:
			return parseDiscordCloseError(payload)
		case discordIPCOpFrame:
			var env discordEnvelope
			if err := json.Unmarshal(payload, &env); err != nil {
				return fmt.Errorf("discord handshake decode: %w", err)
			}
			if env.Evt == "ERROR" {
				return parseDiscordError(env.Data)
			}
			if env.Evt == "READY" {
				return nil
			}
		}
	}
}

func (d *Discord) doSetActivityLocked(ctx context.Context, act *discordActivity) error {
	d.nonce++
	nonce := strconv.FormatUint(d.nonce, 10)

	cmd := discordCommand{
		Cmd: "SET_ACTIVITY",
		Args: discordSetActivityArgs{
			PID:      d.pid,
			Activity: act,
		},
		Nonce: nonce,
	}
	raw, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	conn := d.conn
	stopWatch := watchContextConn(ctx, conn)
	defer stopWatch()

	if err := writeDiscordFrame(conn, discordIPCOpFrame, raw); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("discord write SET_ACTIVITY: %w", err)
	}

	for {
		op, payload, err := readDiscordFrame(conn)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("discord read SET_ACTIVITY response: %w", err)
		}
		switch op {
		case discordIPCOpPing:
			if err := writeDiscordFrame(conn, discordIPCOpPong, payload); err != nil {
				return fmt.Errorf("discord pong: %w", err)
			}
		case discordIPCOpClose:
			return parseDiscordCloseError(payload)
		case discordIPCOpFrame:
			var env discordEnvelope
			if err := json.Unmarshal(payload, &env); err != nil {
				return fmt.Errorf("discord decode response: %w", err)
			}
			if env.Nonce != nonce {
				// Ignore unrelated asynchronous DISPATCH notifications.
				continue
			}
			if env.Evt == "ERROR" {
				return parseDiscordError(env.Data)
			}
			return nil
		}
	}
}

type deadlineSetter interface {
	SetDeadline(time.Time) error
}

// watchContextConn applies any context deadline to conn and closes conn if ctx
// is canceled while an IPC exchange is blocked. The returned stop function
// cleans up the watcher goroutine and clears the deadline before returning.
func watchContextConn(ctx context.Context, conn io.ReadWriteCloser) func() {
	if ds, ok := conn.(deadlineSetter); ok {
		if dl, hasDL := ctx.Deadline(); hasDL {
			_ = ds.SetDeadline(dl)
		}
	}
	ctxDone := ctx.Done()
	if ctxDone == nil {
		return func() {
			if ds, ok := conn.(deadlineSetter); ok {
				_ = ds.SetDeadline(time.Time{})
			}
		}
	}
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		select {
		case <-done:
		case <-ctxDone:
			_ = conn.Close()
		}
	}()
	return func() {
		close(done)
		<-exited
		if ds, ok := conn.(deadlineSetter); ok {
			_ = ds.SetDeadline(time.Time{})
		}
	}
}

func writeDiscordFrame(w io.Writer, op uint32, payload []byte) error {
	var header [8]byte
	binary.LittleEndian.PutUint32(header[0:4], op)
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func readDiscordFrame(r io.Reader) (uint32, []byte, error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	op := binary.LittleEndian.Uint32(header[0:4])
	length := binary.LittleEndian.Uint32(header[4:8])
	if length > maxDiscordFrameBytes {
		return 0, nil, fmt.Errorf("discord IPC frame too large: %d bytes", length)
	}
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	return op, payload, nil
}

type discordHandshake struct {
	Version  int    `json:"v"`
	ClientID string `json:"client_id"`
}

type discordCommand struct {
	Cmd   string                 `json:"cmd"`
	Args  discordSetActivityArgs `json:"args"`
	Nonce string                 `json:"nonce"`
}

type discordSetActivityArgs struct {
	PID      int              `json:"pid"`
	Activity *discordActivity `json:"activity"`
}

type discordActivity struct {
	Details    string             `json:"details,omitempty"`
	State      string             `json:"state,omitempty"`
	Timestamps *discordTimestamps `json:"timestamps,omitempty"`
	Assets     *discordAssets     `json:"assets,omitempty"`
	Party      *discordParty      `json:"party,omitempty"`
	Secrets    *discordSecrets    `json:"secrets,omitempty"`
}

type discordTimestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

type discordAssets struct {
	LargeImage string `json:"large_image,omitempty"`
	LargeText  string `json:"large_text,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
}

type discordParty struct {
	ID   string  `json:"id,omitempty"`
	Size *[2]int `json:"size,omitempty"`
}

type discordSecrets struct {
	Join string `json:"join,omitempty"`
}

type discordEnvelope struct {
	Cmd   string          `json:"cmd"`
	Evt   string          `json:"evt"`
	Nonce string          `json:"nonce"`
	Data  json.RawMessage `json:"data"`
}

type discordErrorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type discordProtocolError struct {
	Code    int
	Message string
}

func (e *discordProtocolError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("discord IPC error %d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("discord IPC error %d", e.Code)
}

func parseDiscordError(raw json.RawMessage) error {
	var ep discordErrorPayload
	_ = json.Unmarshal(raw, &ep)
	return &discordProtocolError{Code: ep.Code, Message: ep.Message}
}

func parseDiscordCloseError(raw []byte) error {
	var ep discordErrorPayload
	_ = json.Unmarshal(raw, &ep)
	if ep.Code != 0 || ep.Message != "" {
		return &discordProtocolError{Code: ep.Code, Message: ep.Message}
	}
	return errors.New("discord IPC closed by peer")
}

func buildDiscordActivity(a Activity) *discordActivity {
	details, state := a.discordLines()
	act := &discordActivity{
		Details: truncateUTF8(details, DiscordMaxTextLength),
		State:   truncateUTF8(state, DiscordMaxTextLength),
	}

	if !a.StartTime.IsZero() || !a.EndTime.IsZero() {
		ts := &discordTimestamps{}
		if !a.StartTime.IsZero() {
			ts.Start = a.StartTime.Unix()
		}
		if !a.EndTime.IsZero() {
			ts.End = a.EndTime.Unix()
		}
		act.Timestamps = ts
	}

	if a.LargeImage != "" || a.LargeText != "" || a.SmallImage != "" || a.SmallText != "" {
		act.Assets = &discordAssets{
			LargeImage: a.LargeImage,
			LargeText:  truncateUTF8(a.LargeText, DiscordMaxTextLength),
			SmallImage: a.SmallImage,
			SmallText:  truncateUTF8(a.SmallText, DiscordMaxTextLength),
		}
	}

	if a.PartyID != "" || a.PartySize > 0 {
		party := &discordParty{ID: a.PartyID}
		if a.PartySize > 0 {
			max := a.PartyMax
			if max < a.PartySize {
				max = a.PartySize
			}
			size := [2]int{a.PartySize, max}
			party.Size = &size
		}
		act.Party = party
	}

	if a.JoinSecret != "" {
		act.Secrets = &discordSecrets{Join: a.JoinSecret}
	}

	return act
}
