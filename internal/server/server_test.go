package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/nawocci/kioku-sync/internal/keys"
	pb "github.com/nawocci/kioku-sync/internal/pb"
	"github.com/nawocci/kioku-sync/internal/store"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	// Keys are minted from the console only; the test does the same.
	key, err := keys.CreateKey(st.DB(), "test")
	if err != nil {
		t.Fatal(err)
	}

	srv := New(st, Options{})
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	return srv, httpServer, key
}

// device is a connected test client.
type device struct {
	t    *testing.T
	conn *websocket.Conn
	ctx  context.Context
}

func dial(t *testing.T, httpServer *httptest.Server, key, deviceID string, lastSeq int64) *device {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, httpServer.URL+"/ws", &websocket.DialOptions{
		HTTPHeader: toHeader(key),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	d := &device{t: t, conn: conn, ctx: ctx}
	d.send(&pb.Frame{Body: &pb.Frame_HelloRequest{
		HelloRequest: &pb.HelloRequest{
			DeviceId: deviceID, DeviceName: deviceID, AppVersion: "test", LastSeq: lastSeq,
		},
	}})
	// A fresh device asks for the snapshot after hello, like the app does.
	if lastSeq <= 0 {
		d.send(snapshotRequestFrame())
	}
	return d
}

func snapshotRequestFrame() *pb.Frame {
	return &pb.Frame{Body: &pb.Frame_SnapshotRequest{}}
}

func toHeader(key string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+key)
	h.Set(protocolHeader, ProtocolVersion)
	return h
}

func (d *device) send(frame *pb.Frame) {
	d.t.Helper()
	blob, err := proto.Marshal(frame)
	if err != nil {
		d.t.Fatal(err)
	}
	if err := d.conn.Write(d.ctx, websocket.MessageBinary, blob); err != nil {
		d.t.Fatal(err)
	}
}

func (d *device) recv() *pb.Frame {
	d.t.Helper()
	typ, data, err := d.conn.Read(d.ctx)
	if err != nil {
		d.t.Fatal(err)
	}
	if typ != websocket.MessageBinary {
		d.t.Fatalf("unexpected text frame")
	}
	var frame pb.Frame
	if err := proto.Unmarshal(data, &frame); err != nil {
		d.t.Fatal(err)
	}
	return &frame
}

// drain reads frames until a predicate matches, failing on timeouts.
func (d *device) drain(match func(*pb.Frame) bool) *pb.Frame {
	d.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			d.t.Fatal("timed out waiting for frame")
		default:
		}
		frame := d.recv()
		if match(frame) {
			return frame
		}
	}
}

func isAck(f *pb.Frame) bool       { return f.GetAck() != nil }
func isEvents(f *pb.Frame) bool    { return f.GetEvents() != nil }
func isSnapshot(f *pb.Frame) bool  { return f.GetSnapshotResponse() != nil }
func isHelloResp(f *pb.Frame) bool { return f.GetHelloResponse() != nil }

func mangaChange(url string, wall int64) *pb.Change {
	return &pb.Change{
		Key: &pb.RecordKey{Kind: &pb.RecordKey_Manga{
			Manga: &pb.MangaKey{SourceId: 1, Url: url},
		}},
		Modified: &pb.Hlc{WallMs: wall, Device: "d1"},
		State:    &pb.Change_Manga{Manga: &pb.MangaState{Title: "t" + url}},
	}
}

func TestLiveFanoutBetweenDevices(t *testing.T) {
	_, httpServer, key := newTestServer(t)

	a := dial(t, httpServer, key, "phone", 0)
	hello := a.drain(isHelloResp)
	if !hello.GetHelloResponse().GetSnapshotRequired() {
		t.Fatal("fresh device was not offered a snapshot")
	}
	a.drain(isSnapshot)

	b := dial(t, httpServer, key, "tablet", 0)
	b.drain(isHelloResp)
	b.drain(isSnapshot)

	// Phone marks a manga read; tablet hears about it live.
	a.send(&pb.Frame{Body: &pb.Frame_ChangeBatch{ChangeBatch: &pb.ChangeBatch{
		Changes: []*pb.Change{mangaChange("m1", 1000)},
	}}})
	ack := a.drain(isAck)
	if len(ack.GetAck().GetItems()) != 1 || !ack.GetAck().GetItems()[0].GetApplied() {
		t.Fatalf("unexpected ack: %v", ack.GetAck())
	}
	events := b.drain(isEvents).GetEvents().GetEvents()
	if len(events) != 1 || events[0].Seq != 1 {
		t.Fatalf("tablet did not receive the event: %v", events)
	}
	if events[0].GetChange().GetManga().GetTitle() != "tm1" {
		t.Fatalf("wrong event payload: %v", events[0])
	}
}

func TestResumeFromLastSeq(t *testing.T) {
	_, httpServer, key := newTestServer(t)

	// Device A writes two changes while B is offline.
	a := dial(t, httpServer, key, "phone", 0)
	a.drain(isHelloResp)
	a.drain(isSnapshot)
	a.send(&pb.Frame{Body: &pb.Frame_ChangeBatch{ChangeBatch: &pb.ChangeBatch{
		Changes: []*pb.Change{mangaChange("m1", 1000)},
	}}})
	a.drain(isAck)
	a.send(&pb.Frame{Body: &pb.Frame_ChangeBatch{ChangeBatch: &pb.ChangeBatch{
		Changes: []*pb.Change{mangaChange("m2", 2000)},
	}}})
	a.drain(isAck)

	// B reconnects having already seen seq 1 and receives only seq 2.
	b := dial(t, httpServer, key, "tablet", 1)
	b.drain(isHelloResp)
	events := b.drain(isEvents).GetEvents().GetEvents()
	if len(events) != 1 || events[0].Seq != 2 {
		t.Fatalf("resume delivered %v, want only seq 2", events)
	}
}

func TestSecondDeviceRejectsStaleKey(t *testing.T) {
	_, httpServer, _ := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, httpServer.URL+"/ws", &websocket.DialOptions{
		HTTPHeader: toHeader("kioku_notavalidkey000000000000000000000000000"),
	})
	if err == nil {
		t.Fatal("invalid key accepted")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestDashboardEndpoints(t *testing.T) {
	_, httpServer, key := newTestServer(t)

	a := dial(t, httpServer, key, "phone", 0)
	a.drain(isHelloResp)
	a.drain(isSnapshot)
	a.send(&pb.Frame{Body: &pb.Frame_ChangeBatch{ChangeBatch: &pb.ChangeBatch{
		Changes: []*pb.Change{mangaChange("m1", 1000)},
	}}})
	a.drain(isAck)

	for _, path := range []string{"/api/v1/library", "/api/v1/devices", "/api/v1/events"} {
		req, _ := http.NewRequest(http.MethodGet, httpServer.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d", path, resp.StatusCode)
		}
		var body json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("GET %s body: %v", path, err)
		}
		resp.Body.Close()
	}

	// Without a key the dashboard denies access.
	resp, err := http.Get(httpServer.URL + "/api/v1/library")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", resp.StatusCode)
	}
}

func TestPingPong(t *testing.T) {
	_, httpServer, key := newTestServer(t)

	a := dial(t, httpServer, key, "phone", 0)
	a.drain(isHelloResp)
	a.drain(isSnapshot)
	a.send(&pb.Frame{Body: &pb.Frame_Ping{Ping: &pb.Ping{WallMs: 42}}})
	pong := a.drain(func(f *pb.Frame) bool { return f.GetPong() != nil })
	if pong.GetPong().GetWallMs() != 42 {
		t.Fatalf("pong wall = %d", pong.GetPong().GetWallMs())
	}
}

func TestBatchAckCarriesSequencePositions(t *testing.T) {
	_, httpServer, key := newTestServer(t)

	a := dial(t, httpServer, key, "phone", 0)
	a.drain(isHelloResp)
	snap := a.drain(isSnapshot)
	if snap.GetSnapshotResponse().GetLatestSeq() != 0 {
		t.Fatalf("fresh library latest_seq = %d", snap.GetSnapshotResponse().GetLatestSeq())
	}
	// After the empty snapshot, the session is live.
	a.send(&pb.Frame{Body: &pb.Frame_ChangeBatch{ChangeBatch: &pb.ChangeBatch{
		Changes: []*pb.Change{mangaChange("m1", 1000)},
	}}})
	ack := a.drain(isAck)
	if ack.GetAck().GetItems()[0].GetApplied() != true {
		t.Fatal("change not applied")
	}
}

func TestHelloWithoutDeviceIdIsRejected(t *testing.T) {
	_, httpServer, key := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, httpServer.URL+"/ws", &websocket.DialOptions{
		HTTPHeader: toHeader(key),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	blob, _ := proto.Marshal(&pb.Frame{Body: &pb.Frame_HelloRequest{
		HelloRequest: &pb.HelloRequest{DeviceId: ""},
	}})
	if err := conn.Write(ctx, websocket.MessageBinary, blob); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var frame pb.Frame
	if err := proto.Unmarshal(data, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.GetHelloResponse().GetError() == "" {
		t.Fatal("hello without device id accepted")
	}
}

func TestHistoryUnionFanout(t *testing.T) {
	_, httpServer, key := newTestServer(t)

	readAt := int64(5000)
	change := &pb.Change{
		Key: &pb.RecordKey{Kind: &pb.RecordKey_History{
			History: &pb.HistoryKey{
				Manga:      &pb.MangaKey{SourceId: 1, Url: "m1"},
				ChapterUrl: "c1",
			},
		}},
		Modified: &pb.Hlc{WallMs: 1000, Device: "phone"},
		State:    &pb.Change_History{History: &pb.HistoryState{ReadAt: &readAt, ReadDuration: 100}},
	}

	a := dial(t, httpServer, key, "phone", 0)
	a.drain(isHelloResp)
	a.drain(isSnapshot)
	a.send(&pb.Frame{Body: &pb.Frame_ChangeBatch{ChangeBatch: &pb.ChangeBatch{
		Changes: []*pb.Change{change},
	}}})
	a.drain(isAck)

	// A second device reading longer unions the durations.
	change2 := proto.Clone(change).(*pb.Change)
	change2.Modified = &pb.Hlc{WallMs: 2000, Device: "tablet"}
	change2.GetHistory().ReadDuration = 300
	b := dial(t, httpServer, key, "tablet", 1)
	b.drain(isHelloResp)
	b.send(&pb.Frame{Body: &pb.Frame_ChangeBatch{ChangeBatch: &pb.ChangeBatch{
		Changes: []*pb.Change{change2},
	}}})
	b.drain(isAck)

	events := a.drain(isEvents).GetEvents().GetEvents()
	if len(events) == 0 {
		t.Fatal("phone did not receive the unioned history")
	}
	h := events[0].GetChange().GetHistory()
	if h.GetReadDuration() != 300 {
		t.Fatalf("unioned duration = %d, want 300", h.GetReadDuration())
	}
	if h.ReadAt == nil || *h.ReadAt != 5000 {
		t.Fatalf("unioned read_at = %v, want 5000", h.ReadAt)
	}
}
