package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestLogRingBufferDropOldestAndCursorResume(t *testing.T) {
	gamedebug.ClearPanic()
	t.Cleanup(gamedebug.ClearPanic)

	var downstream bytes.Buffer
	buf := mcp.NewLogBuffer(mcp.LogBufferOptions{
		Capacity:     5,
		MaxLineBytes: 64,
		Next:         slog.NewTextHandler(&downstream, &slog.HandlerOptions{Level: slog.LevelDebug}),
	})
	logger := slog.New(buf).With("system", "physics").WithGroup("body")

	b := mcp.New(mcp.Config{
		Log: &mcp.LogConfig{
			Buffer: buf,
		},
	})

	callLog := func(args map[string]any) mcp.LogReadResult {
		t.Helper()
		payload, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := b.CallTool(context.Background(), mcp.ToolLogRead, payload)
		if err != nil {
			t.Fatalf("game_log_read(%s): %v", payload, err)
		}
		var res mcp.LogReadResult
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatal(err)
		}
		return res
	}

	// Emit 3 lines and read them with cursor=0.
	logger.Debug("booting", "step", 1)
	logger.Info("spawned hero", "x", 10, "err", errors.New("minor"))
	logger.Warn("high latency", "ms", math.NaN())

	r1 := callLog(map[string]any{})
	if !r1.Configured || r1.Returned != 3 || r1.NextCursor != 3 || r1.OldestSeq != 1 || r1.LatestSeq != 3 || r1.Dropped != 0 || r1.Missed != 0 {
		t.Fatalf("r1 = %+v", r1)
	}
	if r1.Entries[0].Message != "booting" || r1.Entries[1].Attrs["system"] != "physics" || r1.Entries[1].Attrs["body.x"] != float64(10) || r1.Entries[2].Attrs["body.ms"] != "NaN" {
		t.Fatalf("entries = %+v", r1.Entries)
	}
	if !strings.Contains(downstream.String(), "spawned hero") {
		t.Fatalf("downstream tee handler did not receive log: %s", downstream.String())
	}

	// Polling again with next_cursor returns 0 entries and keeps cursor steady.
	rEmpty := callLog(map[string]any{"since": r1.NextCursor})
	if rEmpty.Returned != 0 || len(rEmpty.Entries) != 0 || rEmpty.NextCursor != 3 || rEmpty.HasMore {
		t.Fatalf("rEmpty = %+v", rEmpty)
	}

	// Emit 2 more lines (total 5 = capacity) and paginate with limit=1 from cursor=3.
	logger.Info("frame 4")
	logger.Error("collision failed", "id", "wall_7")

	p1 := callLog(map[string]any{"since": r1.NextCursor, "limit": 1})
	if p1.Returned != 1 || p1.Entries[0].Seq != 4 || p1.NextCursor != 4 || !p1.HasMore {
		t.Fatalf("p1 = %+v", p1)
	}
	p2 := callLog(map[string]any{"since": p1.NextCursor, "limit": 1})
	if p2.Returned != 1 || p2.Entries[0].Seq != 5 || p2.NextCursor != 5 || p2.HasMore {
		t.Fatalf("p2 = %+v", p2)
	}

	// Emit 4 more lines (seq 6..9) so capacity=5 drops oldest 4 entries (seq 1..4)!
	for i := 6; i <= 9; i++ {
		logger.Info(fmt.Sprintf("msg-%d", i))
	}
	if buf.Dropped() != 4 || buf.Len() != 5 || buf.LatestSeq() != 9 {
		t.Fatalf("buffer stats: dropped=%d len=%d latest=%d", buf.Dropped(), buf.Len(), buf.LatestSeq())
	}

	// Reading from stale cursor=2 reports missed=2 (seq 3 and 4 dropped) and resumes at seq 5..9!
	resumed := callLog(map[string]any{"since": 2})
	if resumed.Dropped != 4 || resumed.OldestSeq != 5 || resumed.Missed != 2 || resumed.Returned != 5 || resumed.NextCursor != 9 {
		t.Fatalf("resumed = %+v", resumed)
	}
	if resumed.Entries[0].Seq != 5 || resumed.Entries[4].Seq != 9 {
		t.Fatalf("resumed seq range = %d..%d, want 5..9", resumed.Entries[0].Seq, resumed.Entries[4].Seq)
	}

	// Tail read without cursor (since=0, limit=2) returns the 2 most recent lines (8, 9) and sets truncated=true.
	tail := callLog(map[string]any{"limit": 2})
	if tail.Returned != 2 || !tail.Truncated || tail.HasMore || tail.Entries[0].Seq != 8 || tail.Entries[1].Seq != 9 || tail.NextCursor != 9 {
		t.Fatalf("tail = %+v", tail)
	}

	// Level and substring filtering.
	errOnly := callLog(map[string]any{"level": "error"})
	if errOnly.Returned != 1 || errOnly.Entries[0].Seq != 5 || errOnly.Entries[0].Level != "ERROR" {
		t.Fatalf("errOnly = %+v", errOnly)
	}
	subMatch := callLog(map[string]any{"contains": "WALL_7"})
	if subMatch.Returned != 1 || subMatch.Entries[0].Seq != 5 {
		t.Fatalf("subMatch = %+v", subMatch)
	}

	// Oversized log message is truncated safely.
	logger.Info(strings.Repeat("a", 200))
	longRes := callLog(map[string]any{"limit": 1})
	if len(longRes.Entries[0].Message) > 64+3 || !strings.HasSuffix(longRes.Entries[0].Message, "...") {
		t.Fatalf("expected truncated message, got len=%d %q", len(longRes.Entries[0].Message), longRes.Entries[0].Message)
	}
}

func TestLogAttachInstallsAndRestoresDefaultLoggerAndReportsPanic(t *testing.T) {
	gamedebug.ClearPanic()
	t.Cleanup(gamedebug.ClearPanic)

	prevDefault := slog.Default()
	var captured bytes.Buffer
	baseDefault := slog.New(slog.NewTextHandler(&captured, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(baseDefault)
	t.Cleanup(func() { slog.SetDefault(prevDefault) })

	var (
		cx     *core.Scope
		bridge *mcp.Bridge
	)
	app := nsgotest.Start(t, func(s *core.Scope) core.Node {
		cx = s
		var err error
		bridge, err = mcp.Attach(s, mcp.Config{
			Log: &mcp.LogConfig{Capacity: 16},
		})
		if err != nil {
			t.Fatalf("mcp.Attach: %v", err)
		}
		return core.View{}
	})

	if slog.Default() == baseDefault {
		t.Fatal("mcp.Attach did not install tee handler on slog.Default()")
	}

	slog.Info("hello from default logger", "room", "boss")
	if !strings.Contains(captured.String(), "hello from default logger") {
		t.Fatalf("previous default handler did not receive log: %s", captured.String())
	}

	// Record a recovered panic via pkg/tooling/gamedebug.
	_, panicked := gamedebug.RecoverFunc("loop.Update", func() {
		panic("divide by zero in ai")
	})
	if !panicked {
		t.Fatal("expected RecoverFunc to report panic")
	}

	res := callTool[mcp.LogReadResult](t, app, cx, mcp.ToolLogRead, nil)
	if !res.Configured || res.Returned != 1 || res.Entries[0].Message != "hello from default logger" {
		t.Fatalf("game_log_read = %+v", res)
	}
	if res.LastPanic == nil || res.LastPanic.Value != "divide by zero in ai" || res.LastPanic.Context != "loop.Update" {
		t.Fatalf("last_panic = %+v", res.LastPanic)
	}

	// Detach restores the previous slog.Default().
	app.Post(func() {
		bridge.Detach()
	})
	app.Settle()
	if slog.Default() != baseDefault {
		t.Fatal("bridge.Detach did not restore previous slog.Default()")
	}

	// Unconfigured bridge (Config.Log == nil) still reports configured=false and surfaces gamedebug.LastPanic().
	unconf := mcp.New(mcp.Config{})
	raw, err := unconf.CallTool(context.Background(), mcp.ToolLogRead, nil)
	if err != nil {
		t.Fatalf("unconfigured game_log_read: %v", err)
	}
	var unconfRes mcp.LogReadResult
	if err := json.Unmarshal(raw, &unconfRes); err != nil {
		t.Fatal(err)
	}
	if unconfRes.Configured || unconfRes.LastPanic == nil || unconfRes.LastPanic.Value != "divide by zero in ai" {
		t.Fatalf("unconfigured game_log_read = %+v", unconfRes)
	}
}

func TestLogBufferConcurrentRaceAndValidation(t *testing.T) {
	buf := mcp.NewLogBuffer(mcp.LogBufferOptions{Capacity: 32})
	b := mcp.New(mcp.Config{
		Log: &mcp.LogConfig{
			Buffer: buf,
			LastPanic: func() (gamedebug.PanicRecord, bool) {
				return gamedebug.PanicRecord{Value: "custom-panic", Context: "test"}, true
			},
		},
	})

	var wg sync.WaitGroup
	for g := range 6 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			l := slog.New(buf).With("worker", id).WithGroup("job")
			for i := range 100 {
				l.Info("tick", "i", i)
			}
		}(g)
	}
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var cursor uint64
			for range 50 {
				res := buf.Read(mcp.LogReadOptions{Since: cursor, Limit: 10})
				cursor = res.NextCursor
			}
		}()
	}
	wg.Wait()

	if buf.LatestSeq() != 600 || buf.Len() != 32 || buf.Dropped() != 600-32 {
		t.Fatalf("after stress: latest=%d len=%d dropped=%d", buf.LatestSeq(), buf.Len(), buf.Dropped())
	}

	raw, err := b.CallTool(context.Background(), mcp.ToolLogRead, json.RawMessage(`{"limit":5}`))
	if err != nil {
		t.Fatal(err)
	}
	var res mcp.LogReadResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if res.LastPanic == nil || res.LastPanic.Value != "custom-panic" {
		t.Fatalf("custom LastPanic = %+v", res.LastPanic)
	}

	// Validation errors.
	for _, bad := range []string{
		`{"level":"bogus"}`,
		`{"limit":9999}`,
		`{"limit":-1}`,
		`{"unknown":1}`,
	} {
		if _, err := b.CallTool(context.Background(), mcp.ToolLogRead, json.RawMessage(bad)); err == nil {
			t.Errorf("game_log_read(%s) succeeded, want error", bad)
		}
	}
}
