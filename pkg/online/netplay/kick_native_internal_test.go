//go:build !js

package netplay

import (
	"errors"
	"testing"
	"time"
)

func TestWebSocketServerKick(t *testing.T) {
	clock := newFakeClock()
	srv, url := listenWS(t, tokenAuth(clock, time.Minute, nil))
	cli, err := DialWebSocket(url, []byte("good"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cli.Close() }()
	id := cli.LocalID()
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: id}) {
		t.Fatalf("event = %+v, want joined", ev)
	}

	if err := srv.Kick(0, "nobody"); !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("kick of an unknown peer = %v, want ErrUnknownPeer", err)
	}
	if err := srv.Kick(id, "banned"); err != nil {
		t.Fatalf("kick: %v", err)
	}
	awaitChange(t, cli.changed, "the client to notice the kick", func() bool { return cli.State() == ConnDisconnected })
	var kick *KickError
	if err := cli.Err(); !errors.As(err, &kick) || kick.Reason != "banned" || !errors.Is(err, ErrKicked) {
		t.Fatalf("client Err = %v, want a KickError with the reason", err)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerLeft, Peer: id, Reason: "banned"}) {
		t.Fatalf("event = %+v, want left with the reason", ev)
	}
	if err := srv.Send(id, ChannelReliableOrdered, []byte("x")); !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("send after kick = %v, want ErrUnknownPeer", err)
	}
	if err := srv.Kick(id, "again"); !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("second kick = %v, want ErrUnknownPeer", err)
	}

	// The session is gone with its secret: a reconnect joins as a new peer, it cannot resume.
	if err := cli.Reconnect(); err != nil {
		t.Fatalf("reconnect after kick: %v", err)
	}
	if cli.LocalID() == id || cli.Resumed() {
		t.Fatalf("reconnect after kick kept ID %d (resumed=%v)", cli.LocalID(), cli.Resumed())
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: cli.LocalID()}) {
		t.Fatalf("event = %+v, want a new join", ev)
	}
}

func TestWebSocketServerKickDuringGracePeriod(t *testing.T) {
	clock := newFakeClock()
	srv, url := listenWS(t, tokenAuth(clock, time.Minute, nil))
	cli, err := DialWebSocket(url, []byte("good"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cli.Close() }()
	id := cli.LocalID()
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: id}) {
		t.Fatalf("event = %+v, want joined", ev)
	}

	cutWS(cli)
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerDropped, Peer: id}) {
		t.Fatalf("event = %+v, want dropped", ev)
	}
	if err := srv.Kick(id, "afk"); err != nil {
		t.Fatalf("kick during grace: %v", err)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerLeft, Peer: id, Reason: "afk"}) {
		t.Fatalf("event = %+v, want left with the reason", ev)
	}

	// A reconnect after the grace-period kick is a new peer, never a resume.
	awaitChange(t, cli.changed, "the client to notice the drop", func() bool { return cli.State() == ConnDisconnected })
	if err := cli.Reconnect(); err != nil {
		t.Fatalf("reconnect after kick: %v", err)
	}
	if cli.LocalID() == id || cli.Resumed() {
		t.Fatalf("reconnect after kick kept ID %d (resumed=%v)", cli.LocalID(), cli.Resumed())
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: cli.LocalID()}) {
		t.Fatalf("event = %+v, want a new join", ev)
	}
}

func TestUDPServerKick(t *testing.T) {
	clock := newFakeClock()
	srv := listenUDPServer(t, tokenAuth(clock, time.Minute, nil))
	cli, err := DialUDP(srv.Addr().String(), srv.PublicKey(), []byte("good"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cli.Close() }()
	id := cli.LocalID()
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: id}) {
		t.Fatalf("event = %+v, want joined", ev)
	}

	if err := srv.Kick(0, "nobody"); !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("kick of an unknown peer = %v, want ErrUnknownPeer", err)
	}
	if err := srv.Kick(id, "banned"); err != nil {
		t.Fatalf("kick: %v", err)
	}
	awaitChange(t, cli.changed, "the client to notice the kick", func() bool { return cli.State() == ConnDisconnected })
	var kick *KickError
	if err := cli.Err(); !errors.As(err, &kick) || kick.Reason != "banned" || !errors.Is(err, ErrKicked) {
		t.Fatalf("client Err = %v, want a KickError with the reason", err)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerLeft, Peer: id, Reason: "banned"}) {
		t.Fatalf("event = %+v, want left with the reason", ev)
	}
	if err := srv.Send(id, ChannelUnreliable, []byte("x")); !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("send after kick = %v, want ErrUnknownPeer", err)
	}
	if err := srv.Kick(id, "again"); !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("second kick = %v, want ErrUnknownPeer", err)
	}

	// The session is gone with its secret: a reconnect joins as a new peer, it cannot resume.
	if err := cli.Reconnect(); err != nil {
		t.Fatalf("reconnect after kick: %v", err)
	}
	if cli.LocalID() == id || cli.Resumed() {
		t.Fatalf("reconnect after kick kept ID %d (resumed=%v)", cli.LocalID(), cli.Resumed())
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: cli.LocalID()}) {
		t.Fatalf("event = %+v, want a new join", ev)
	}
}

func TestUDPServerKickDuringGracePeriod(t *testing.T) {
	clock := newFakeClock()
	auth := tokenAuth(clock, time.Minute, nil)
	auth.IdleTimeout = 3 * time.Second
	srv := listenUDPServer(t, auth)
	cli, err := DialUDP(srv.Addr().String(), srv.PublicKey(), []byte("good"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cli.Close() }()
	id := cli.LocalID()
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: id}) {
		t.Fatalf("event = %+v, want joined", ev)
	}

	cutUDP(cli)
	clock.Advance(3 * time.Second)
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerDropped, Peer: id}) {
		t.Fatalf("event = %+v, want dropped after the idle timeout", ev)
	}
	if err := srv.Kick(id, "afk"); err != nil {
		t.Fatalf("kick during grace: %v", err)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerLeft, Peer: id, Reason: "afk"}) {
		t.Fatalf("event = %+v, want left with the reason", ev)
	}

	// A reconnect after the grace-period kick is a new peer, never a resume.
	if err := cli.Reconnect(); err != nil {
		t.Fatalf("reconnect after kick: %v", err)
	}
	if cli.LocalID() == id || cli.Resumed() {
		t.Fatalf("reconnect after kick kept ID %d (resumed=%v)", cli.LocalID(), cli.Resumed())
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: cli.LocalID()}) {
		t.Fatalf("event = %+v, want a new join", ev)
	}
}
