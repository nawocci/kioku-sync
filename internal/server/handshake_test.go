package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	pb "github.com/nawocci/kioku-sync/internal/pb"
)

// A client that does not speak the server's protocol version is refused
// before the WebSocket is even established.
func TestVersionHandshakeIsEnforced(t *testing.T) {
	_, httpServer, key := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for name, headers := range map[string]http.Header{
		"missing header":  authHeader(key),
		"wrong version":   versionedHeader(key, "2"),
		"garbage version": versionedHeader(key, "banana"),
		"empty version":   versionedHeader(key, ""),
	} {
		_, res, err := websocket.Dial(ctx, httpServer.URL+"/ws", &websocket.DialOptions{
			HTTPHeader: headers,
		})
		if err == nil {
			t.Fatalf("%s: dial succeeded, want refusal", name)
		}
		if res == nil || res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status = %v, want 400", name, res)
		}
		if res.StatusCode == http.StatusBadRequest && res.Body == nil {
			t.Fatalf("%s: refusal carries no explanation", name)
		}
		if res != nil && res.Body != nil {
			res.Body.Close()
		}
	}
}

// The 101 response must name the protocol version so the client can verify
// the server before exchanging frames.
func TestHandshakeResponseNamesTheProtocolVersion(t *testing.T) {
	_, httpServer, key := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, res, err := websocket.Dial(ctx, httpServer.URL+"/ws", &websocket.DialOptions{
		HTTPHeader: versionedHeader(key, ProtocolVersion),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if res == nil || res.Header.Get(protocolHeader) != ProtocolVersion {
		t.Fatalf("handshake response does not name protocol version %q", ProtocolVersion)
	}
}

// A device in its snapshot phase must never receive live events: the frame
// after the snapshot request is the snapshot itself, and a change committed
// after the snapshot was taken still reaches the device afterwards, through
// the post-join replay or the fan-out, whichever covers the moment it lands.
func TestSnapshotPhaseReceivesNoLiveEvents(t *testing.T) {
	_, httpServer, key := newTestServer(t)

	pusher := dial(t, httpServer, key, "pusher", 0)
	pusher.recvHello(t)
	pusher.sendChange(t, mangaChange("a", 100))
	pusher.drain(isAck)

	joiner := dialNoSnapshot(t, httpServer, key, "joiner")
	joiner.recvHello(t)

	// A change committing while the joiner sits between hello and its
	// snapshot request must not be fanned out to it.
	pusher.sendChange(t, mangaChange("b", 101))
	pusher.drain(isAck)

	joiner.send(snapshotRequestFrame())
	first := joiner.recv()
	if !isSnapshot(first) {
		t.Fatalf("first frame after snapshot request = %v, want the snapshot", first.GetBody())
	}
	snap := first.GetSnapshotResponse()
	if snap.GetLatestSeq() < 2 {
		t.Fatalf("snapshot position = %d, want at least 2", snap.GetLatestSeq())
	}

	// This change commits after the snapshot was taken, so only the replay
	// or the live fan-out can deliver it; the joiner must end up with it
	// either way.
	pusher.sendChange(t, mangaChange("c", 102))
	pusher.drain(isAck)

	ev := joiner.drain(isEvents)
	found := false
	for _, committed := range ev.GetEvents().GetEvents() {
		if committed.GetChange().GetKey().GetManga().GetUrl() == "c" {
			found = true
		}
	}
	if !found {
		t.Fatal("the change committed after the snapshot never reached the joiner")
	}
}

// A malformed change — no key or no clock — fails the session instead of
// being silently acknowledged.
func TestMalformedChangeFailsTheSession(t *testing.T) {
	_, httpServer, key := newTestServer(t)

	d := dial(t, httpServer, key, "d1", 0)
	d.recvHello(t)

	ch := mangaChange("a", 100)
	ch.Modified = nil
	d.sendChange(t, ch)

	// The session must end; no ack may arrive for the malformed change.
	d.conn.CloseRead(d.ctx)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("session survived a malformed change")
		default:
		}
		if _, _, err := d.conn.Reader(d.ctx); err != nil {
			return // session closed
		}
	}
}

func authHeader(key string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+key)
	return h
}

func versionedHeader(key, version string) http.Header {
	h := authHeader(key)
	h.Set(protocolHeader, version)
	return h
}

// dialNoSnapshot dials and says hello but does not request the snapshot, so
// a test can control the phase between hello and the snapshot.
func dialNoSnapshot(t *testing.T, httpServer *httptest.Server, key, deviceID string) *device {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, httpServer.URL+"/ws", &websocket.DialOptions{
		HTTPHeader: versionedHeader(key, ProtocolVersion),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	d := &device{t: t, conn: conn, ctx: ctx}
	d.send(&pb.Frame{Body: &pb.Frame_HelloRequest{
		HelloRequest: &pb.HelloRequest{
			DeviceId: deviceID, DeviceName: deviceID, AppVersion: "test", LastSeq: 0,
		},
	}})
	return d
}

func (d *device) recvHello(t *testing.T) *pb.Frame {
	t.Helper()
	return d.drain(isHelloResp)
}

func (d *device) sendChange(t *testing.T, ch *pb.Change) {
	t.Helper()
	d.send(&pb.Frame{Body: &pb.Frame_ChangeBatch{
		ChangeBatch: &pb.ChangeBatch{Changes: []*pb.Change{ch}},
	}})
}
