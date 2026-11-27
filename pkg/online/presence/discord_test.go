package presence_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/online/presence"
)

const (
	testOpHandshake uint32 = 0
	testOpFrame     uint32 = 1
	testOpClose     uint32 = 2
	testOpPing      uint32 = 3
	testOpPong      uint32 = 4
)

type recordedDiscordFrame struct {
	Op      uint32
	Payload []byte
}

type mockDiscordServer struct {
	mu           sync.Mutex
	frames       []recordedDiscordFrame
	pongs        [][]byte
	handshakes   int
	sendPingOnce bool
	sendAsyncEvt bool
	nextErrCode  int
	nextErrMsg   string
	rejectClose  bool
	dropAfterSet int
	setCount     int
	wg           sync.WaitGroup
}

func (s *mockDiscordServer) dial(context.Context) (io.ReadWriteCloser, error) {
	client, server := net.Pipe()
	s.wg.Add(1)
	go s.serve(server)
	return client, nil
}

func (s *mockDiscordServer) wait() {
	s.wg.Wait()
}

func (s *mockDiscordServer) recordedFrames() []recordedDiscordFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recordedDiscordFrame, len(s.frames))
	copy(out, s.frames)
	return out
}

func (s *mockDiscordServer) recordedPongs() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.pongs))
	copy(out, s.pongs)
	return out
}

func (s *mockDiscordServer) serve(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	for {
		op, payload, err := readRawFrame(conn)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.frames = append(s.frames, recordedDiscordFrame{Op: op, Payload: payload})
		s.mu.Unlock()

		switch op {
		case testOpHandshake:
			s.mu.Lock()
			s.handshakes++
			reject := s.rejectClose
			sendPing := s.sendPingOnce
			s.sendPingOnce = false
			s.mu.Unlock()

			if reject {
				_ = writeRawFrame(conn, testOpClose, []byte(`{"code":4000,"message":"invalid client id"}`))
				return
			}
			if sendPing {
				_ = writeRawFrame(conn, testOpPing, []byte(`{"ping":42}`))
				pongOp, pongPayload, err := readRawFrame(conn)
				if err != nil {
					return
				}
				if pongOp == testOpPong {
					s.mu.Lock()
					s.pongs = append(s.pongs, pongPayload)
					s.mu.Unlock()
				}
			}
			_ = writeRawFrame(conn, testOpFrame, []byte(`{"cmd":"DISPATCH","evt":"READY","data":{"v":1}}`))

		case testOpFrame:
			var req struct {
				Cmd   string          `json:"cmd"`
				Nonce string          `json:"nonce"`
				Args  json.RawMessage `json:"args"`
			}
			if err := json.Unmarshal(payload, &req); err != nil {
				return
			}

			s.mu.Lock()
			s.setCount++
			drop := s.dropAfterSet > 0 && s.setCount == s.dropAfterSet
			asyncEvt := s.sendAsyncEvt
			s.sendAsyncEvt = false
			errCode := s.nextErrCode
			errMsg := s.nextErrMsg
			s.nextErrCode = 0
			s.nextErrMsg = ""
			s.mu.Unlock()

			if drop {
				// Simulate Discord crashing/restarting mid-command.
				return
			}
			if asyncEvt {
				_ = writeRawFrame(conn, testOpFrame, []byte(`{"cmd":"DISPATCH","evt":"ACTIVITY_JOIN","data":{}}`))
			}
			if errCode != 0 {
				resp, _ := json.Marshal(map[string]any{
					"cmd":   req.Cmd,
					"evt":   "ERROR",
					"nonce": req.Nonce,
					"data":  map[string]any{"code": errCode, "message": errMsg},
				})
				_ = writeRawFrame(conn, testOpFrame, resp)
				continue
			}
			resp, _ := json.Marshal(map[string]any{
				"cmd":   req.Cmd,
				"nonce": req.Nonce,
				"data":  map[string]any{},
			})
			_ = writeRawFrame(conn, testOpFrame, resp)

		case testOpClose:
			return
		}
	}
}

func readRawFrame(r io.Reader) (uint32, []byte, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	op := binary.LittleEndian.Uint32(hdr[0:4])
	n := binary.LittleEndian.Uint32(hdr[4:8])
	buf := make([]byte, n)
	if n > 0 {
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, nil, err
		}
	}
	return op, buf, nil
}

func writeRawFrame(w io.Writer, op uint32, payload []byte) error {
	var hdr [8]byte
	binary.LittleEndian.PutUint32(hdr[0:4], op)
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := w.Write(payload)
		return err
	}
	return nil
}

func TestDiscordHandshakeSetLevelAndClear(t *testing.T) {
	srv := &mockDiscordServer{}
	d, err := presence.NewDiscord(presence.DiscordConfig{
		ClientID: "123456789012345678",
		PID:      4242,
		Dial:     srv.dial,
	})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}

	ctx := context.Background()
	start := time.Unix(1700000000, 0)
	end := time.Unix(1700003600, 0)

	if err := d.Set(ctx, presence.Activity{
		Mode:       "Campaign",
		Level:      "Forest 1-2",
		PartyID:    "squad-1",
		PartySize:  2,
		PartyMax:   4,
		StartTime:  start,
		EndTime:    end,
		LargeImage: "forest_art",
		LargeText:  "Whispering Forest",
		SmallImage: "hero_badge",
		SmallText:  "Level 12",
		JoinSecret: "join-secret-abc",
	}); err != nil {
		t.Fatalf("Set initial: %v", err)
	}

	if err := d.Set(ctx, presence.Activity{
		Mode:      "Campaign",
		Level:     "Cavern 2-1",
		PartyID:   "squad-1",
		PartySize: 2,
		PartyMax:  4,
	}); err != nil {
		t.Fatalf("Set second level: %v", err)
	}

	if err := d.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	srv.wait()

	if err := d.Set(ctx, presence.Activity{Level: "After Close"}); !errors.Is(err, presence.ErrClosed) {
		t.Fatalf("Set after Close: got %v, want ErrClosed", err)
	}
	if err := d.Clear(ctx); !errors.Is(err, presence.ErrClosed) {
		t.Fatalf("Clear after Close: got %v, want ErrClosed", err)
	}

	frames := srv.recordedFrames()
	if len(frames) != 5 {
		t.Fatalf("expected 5 frames (handshake, set1, set2, clear, close), got %d", len(frames))
	}

	// Frame 0: Handshake.
	if frames[0].Op != testOpHandshake {
		t.Fatalf("frame 0 op = %d, want %d", frames[0].Op, testOpHandshake)
	}
	var hs struct {
		V        int    `json:"v"`
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(frames[0].Payload, &hs); err != nil {
		t.Fatalf("unmarshal handshake: %v", err)
	}
	if hs.V != 1 || hs.ClientID != "123456789012345678" {
		t.Fatalf("handshake = %+v, want v=1 client_id=123456789012345678", hs)
	}

	// Frame 1: First SET_ACTIVITY with full payload.
	if frames[1].Op != testOpFrame {
		t.Fatalf("frame 1 op = %d, want %d", frames[1].Op, testOpFrame)
	}
	var set1 struct {
		Cmd   string `json:"cmd"`
		Nonce string `json:"nonce"`
		Args  struct {
			PID      int `json:"pid"`
			Activity struct {
				Details    string `json:"details"`
				State      string `json:"state"`
				Timestamps struct {
					Start int64 `json:"start"`
					End   int64 `json:"end"`
				} `json:"timestamps"`
				Assets struct {
					LargeImage string `json:"large_image"`
					LargeText  string `json:"large_text"`
					SmallImage string `json:"small_image"`
					SmallText  string `json:"small_text"`
				} `json:"assets"`
				Party struct {
					ID   string `json:"id"`
					Size []int  `json:"size"`
				} `json:"party"`
				Secrets struct {
					Join string `json:"join"`
				} `json:"secrets"`
			} `json:"activity"`
		} `json:"args"`
	}
	if err := json.Unmarshal(frames[1].Payload, &set1); err != nil {
		t.Fatalf("unmarshal set1: %v", err)
	}
	if set1.Cmd != "SET_ACTIVITY" || set1.Args.PID != 4242 || set1.Nonce != "1" {
		t.Fatalf("set1 envelope = %+v", set1)
	}
	act := set1.Args.Activity
	if act.Details != "Campaign" || act.State != "Forest 1-2" {
		t.Fatalf("set1 details/state = (%q, %q), want (Campaign, Forest 1-2)", act.Details, act.State)
	}
	if act.Timestamps.Start != start.Unix() || act.Timestamps.End != end.Unix() {
		t.Fatalf("set1 timestamps = %+v", act.Timestamps)
	}
	if act.Assets.LargeImage != "forest_art" || act.Assets.LargeText != "Whispering Forest" ||
		act.Assets.SmallImage != "hero_badge" || act.Assets.SmallText != "Level 12" {
		t.Fatalf("set1 assets = %+v", act.Assets)
	}
	if act.Party.ID != "squad-1" || len(act.Party.Size) != 2 || act.Party.Size[0] != 2 || act.Party.Size[1] != 4 {
		t.Fatalf("set1 party = %+v", act.Party)
	}
	if act.Secrets.Join != "join-secret-abc" {
		t.Fatalf("set1 secrets = %+v", act.Secrets)
	}

	// Frame 2: Second SET_ACTIVITY after changing level.
	var set2 struct {
		Nonce string `json:"nonce"`
		Args  struct {
			Activity struct {
				Details string `json:"details"`
				State   string `json:"state"`
			} `json:"activity"`
		} `json:"args"`
	}
	if err := json.Unmarshal(frames[2].Payload, &set2); err != nil {
		t.Fatalf("unmarshal set2: %v", err)
	}
	if set2.Nonce != "2" || set2.Args.Activity.Details != "Campaign" || set2.Args.Activity.State != "Cavern 2-1" {
		t.Fatalf("set2 = %+v", set2)
	}

	// Frame 3: Clear sends explicit "activity":null.
	var clearMsg struct {
		Cmd  string `json:"cmd"`
		Args struct {
			PID      int             `json:"pid"`
			Activity json.RawMessage `json:"activity"`
		} `json:"args"`
	}
	if err := json.Unmarshal(frames[3].Payload, &clearMsg); err != nil {
		t.Fatalf("unmarshal clear: %v", err)
	}
	if clearMsg.Cmd != "SET_ACTIVITY" || clearMsg.Args.PID != 4242 || string(clearMsg.Args.Activity) != "null" {
		t.Fatalf("clear = cmd:%q pid:%d activity:%s", clearMsg.Cmd, clearMsg.Args.PID, string(clearMsg.Args.Activity))
	}

	// Frame 4: Close frame.
	if frames[4].Op != testOpClose {
		t.Fatalf("frame 4 op = %d, want %d", frames[4].Op, testOpClose)
	}
}

func TestDiscordPingAndAsyncDispatch(t *testing.T) {
	srv := &mockDiscordServer{
		sendPingOnce: true,
		sendAsyncEvt: true,
	}
	d, err := presence.NewDiscord(presence.DiscordConfig{
		ClientID: "app-1",
		Dial:     srv.dial,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = d.Close()
		srv.wait()
	}()

	if err := d.Set(context.Background(), presence.Activity{Level: "Stage 3"}); err != nil {
		t.Fatalf("Set with ping + async event: %v", err)
	}

	pongs := srv.recordedPongs()
	if len(pongs) != 1 || string(pongs[0]) != `{"ping":42}` {
		t.Fatalf("pongs = %q, want [{\"ping\":42}]", pongs)
	}
}

func TestDiscordReconnectOnBrokenPipe(t *testing.T) {
	srv := &mockDiscordServer{dropAfterSet: 2}
	d, err := presence.NewDiscord(presence.DiscordConfig{
		ClientID: "app-reconnect",
		Dial:     srv.dial,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = d.Close()
		srv.wait()
	}()

	ctx := context.Background()
	if err := d.Set(ctx, presence.Activity{Level: "Level 1"}); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	// Second Set hits server drop and should automatically reconnect + handshake + retry.
	if err := d.Set(ctx, presence.Activity{Level: "Level 2"}); err != nil {
		t.Fatalf("second Set (after reconnect): %v", err)
	}

	srv.mu.Lock()
	handshakes := srv.handshakes
	srv.mu.Unlock()
	if handshakes != 2 {
		t.Fatalf("handshakes = %d, want 2 (initial + reconnect)", handshakes)
	}
}

func TestDiscordUnavailableErrorsAndLimits(t *testing.T) {
	if _, err := presence.NewDiscord(presence.DiscordConfig{}); !errors.Is(err, presence.ErrInvalidConfig) {
		t.Fatalf("empty ClientID: got %v, want ErrInvalidConfig", err)
	}

	// Unavailable dialer.
	unavail, err := presence.NewDiscord(presence.DiscordConfig{
		ClientID: "app-unavail",
		Dial: func(context.Context) (io.ReadWriteCloser, error) {
			return nil, presence.ErrUnavailable
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer unavail.Close()

	if err := unavail.Set(context.Background(), presence.Activity{Level: "1-1"}); !errors.Is(err, presence.ErrUnavailable) {
		t.Fatalf("unavailable Set: got %v, want ErrUnavailable", err)
	}
	if err := unavail.Clear(context.Background()); !errors.Is(err, presence.ErrUnavailable) {
		t.Fatalf("unavailable Clear: got %v, want ErrUnavailable", err)
	}

	// Handshake rejected with close frame.
	rejectSrv := &mockDiscordServer{rejectClose: true}
	rejectClient, err := presence.NewDiscord(presence.DiscordConfig{
		ClientID: "bad-id",
		Dial:     rejectSrv.dial,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectClient.Connect(context.Background()); err == nil || !strings.Contains(err.Error(), "4000") {
		t.Fatalf("rejected handshake: got %v, want error containing 4000", err)
	}
	_ = rejectClient.Close()
	rejectSrv.wait()

	// Protocol ERROR on SET_ACTIVITY + UTF-8 truncation + party max clamping.
	srv := &mockDiscordServer{
		nextErrCode: 5000,
		nextErrMsg:  "invalid activity",
	}
	d, err := presence.NewDiscord(presence.DiscordConfig{
		ClientID: "app-err",
		Dial:     srv.dial,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = d.Close()
		srv.wait()
	}()

	if err := d.Set(context.Background(), presence.Activity{Level: "Bad"}); err == nil || !strings.Contains(err.Error(), "5000") {
		t.Fatalf("protocol error: got %v, want error containing 5000", err)
	}

	// Connection remains usable after protocol error; test UTF-8 truncation & party clamp.
	longUTF8 := strings.Repeat("森", 60) // 180 bytes, 3 bytes per rune
	if err := d.Set(context.Background(), presence.Activity{
		Level:     longUTF8,
		PartySize: 3,
		PartyMax:  1, // smaller than PartySize; should clamp to 3
	}); err != nil {
		t.Fatalf("Set after protocol error: %v", err)
	}

	frames := srv.recordedFrames()
	last := frames[len(frames)-1]
	var decoded struct {
		Args struct {
			Activity struct {
				State string `json:"state"`
				Party struct {
					Size []int `json:"size"`
				} `json:"party"`
			} `json:"activity"`
		} `json:"args"`
	}
	if err := json.Unmarshal(last.Payload, &decoded); err != nil {
		t.Fatal(err)
	}
	state := decoded.Args.Activity.State
	if len(state) > presence.DiscordMaxTextLength || !utf8.ValidString(state) {
		t.Fatalf("truncated state len=%d valid=%v", len(state), utf8.ValidString(state))
	}
	if len(decoded.Args.Activity.Party.Size) != 2 || decoded.Args.Activity.Party.Size[0] != 3 || decoded.Args.Activity.Party.Size[1] != 3 {
		t.Fatalf("clamped party size = %v, want [3 3]", decoded.Args.Activity.Party.Size)
	}
}
