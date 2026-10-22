// Package server serves the sync protocol: a WebSocket endpoint for devices,
// and a small read-only REST API for the dashboard.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/nawocci/kioku-sync/internal/engine"
	"github.com/nawocci/kioku-sync/internal/keys"
	pb "github.com/nawocci/kioku-sync/internal/pb"
	"github.com/nawocci/kioku-sync/internal/store"
)

// ProtocolVersion is the sync protocol this server speaks. It is advertised
// on every WebSocket handshake and must match the client exactly; see
// PROTOCOL.md for when it changes.
const ProtocolVersion = "1"

const protocolHeader = "X-Kioku-Sync-Protocol"

// Options configures the server.
type Options struct {
	// Static is the embedded dashboard build; it may be nil while the
	// dashboard is developed.
	Static fs.FS
	// Retention bounds how long the event log is kept before compaction.
	Retention time.Duration
	// PruneEvery controls the background maintenance interval.
	PruneEvery time.Duration
}

// Server is the kioku-sync HTTP server.
type Server struct {
	store  *store.Store
	engine *engine.Engine
	opts   Options

	mu   sync.Mutex
	hubs map[int64]map[*session]struct{} // library id -> live sessions
}

// New creates a server writing to st.
func New(st *store.Store, opts Options) *Server {
	if opts.Retention <= 0 {
		opts.Retention = 30 * 24 * time.Hour
	}
	if opts.PruneEvery <= 0 {
		opts.PruneEvery = time.Hour
	}
	return &Server{
		store:  st,
		engine: engine.New(st),
		opts:   opts,
		hubs:   map[int64]map[*session]struct{}{},
	}
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"protocol": ProtocolVersion}, http.StatusOK)
	})
	mux.Handle("GET /ws", http.HandlerFunc(s.handleWebSocket))
	mux.Handle("GET /api/v1/library", s.auth(s.handleLibrary))
	mux.Handle("GET /api/v1/devices", s.auth(s.handleDevices))
	mux.Handle("GET /api/v1/events", s.auth(s.handleEvents))
	if s.opts.Static != nil {
		mux.Handle("GET /", http.FileServerFS(s.opts.Static))
	}
	return logRequests(mux)
}

// StartMaintenance launches background log compaction until ctx is done.
func (s *Server) StartMaintenance(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.opts.PruneEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.store.PruneEvents(time.Now().Add(-s.opts.Retention).Unix()); err != nil {
					log.Printf("pruning events: %v", err)
				}
			}
		}
	}()
}

// ---------------------------------------------------------------- REST

// auth wraps a dashboard handler with API key authentication and passes the
// library id through the request context.
func (s *Server) auth(next func(w http.ResponseWriter, r *http.Request, library int64)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		library, _, err := keys.Lookup(s.store.DB(), bearer(r))
		if err != nil || library == 0 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="kioku-sync"`)
			http.Error(w, "invalid key", http.StatusUnauthorized)
			return
		}
		next(w, r, library)
	})
}

func bearer(r *http.Request) string {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
		return auth[len(prefix):]
	}
	return ""
}

func (s *Server) handleLibrary(w http.ResponseWriter, _ *http.Request, library int64) {
	info, err := s.store.LibraryInfo(library)
	if err != nil {
		http.Error(w, "library unavailable", http.StatusInternalServerError)
		return
	}
	counts, err := s.store.Counts(library)
	if err != nil {
		http.Error(w, "library unavailable", http.StatusInternalServerError)
		return
	}
	devices, err := s.store.Devices(library)
	if err != nil {
		http.Error(w, "library unavailable", http.StatusInternalServerError)
		return
	}
	online := 0
	s.mu.Lock()
	online = len(s.hubs[library])
	s.mu.Unlock()

	writeJSON(w, map[string]any{
		"label":      info.Label,
		"created_at": info.CreatedAt,
		"last_seq":   info.LastSeq,
		"records":    counts,
		"devices":    len(devices),
		"online":     online,
	}, http.StatusOK)
}

func (s *Server) handleDevices(w http.ResponseWriter, _ *http.Request, library int64) {
	devices, err := s.store.Devices(library)
	if err != nil {
		http.Error(w, "library unavailable", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	hub := s.hubs[library]
	s.mu.Unlock()
	online := map[string]bool{}
	for sess := range hub {
		online[sess.deviceID] = true
	}

	out := make([]map[string]any, 0, len(devices))
	for _, d := range devices {
		out = append(out, map[string]any{
			"id":          d.ID,
			"name":        d.Name,
			"app_version": d.AppVersion,
			"last_seen":   d.LastSeen,
			"last_seq":    d.LastSeq,
			"online":      online[d.ID],
		})
	}
	writeJSON(w, out, http.StatusOK)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request, library int64) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	events, err := s.store.RecentEvents(library, limit)
	if err != nil {
		http.Error(w, "library unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, events, http.StatusOK)
}

func writeJSON(w http.ResponseWriter, body any, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("writing response: %v", err)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		log.Printf("%s %s", r.Method, r.URL.Path)
	})
}

// ---------------------------------------------------------------- WebSocket

type session struct {
	server    *Server
	library   int64
	deviceID  string
	out       chan []byte
	closeOnce sync.Once
	closed    chan struct{}
}

func (s *session) send(blob []byte) bool {
	select {
	case s.out <- blob:
		return true
	case <-s.closed:
		return false
	}
}

func (s *session) shutdown() {
	s.closeOnce.Do(func() { close(s.closed) })
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if v := r.Header.Get(protocolHeader); v != ProtocolVersion {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "this server speaks kioku sync protocol %s, the client offered %q; upgrade Kioku and kioku-sync to matching versions", ProtocolVersion, v)
		return
	}

	library, label, err := keys.Lookup(s.store.DB(), bearer(r))
	if err != nil || library == 0 {
		http.Error(w, "invalid key", http.StatusUnauthorized)
		return
	}

	// Echo the protocol version on the handshake response so the client can
	// verify it before exchanging any frame.
	w.Header().Set(protocolHeader, ProtocolVersion)
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		return
	}
	conn.SetReadLimit(32 << 20)

	sess := &session{
		server:  s,
		library: library,
		out:     make(chan []byte, 256),
		closed:  make(chan struct{}),
	}
	defer func() {
		sess.shutdown()
		s.leave(sess)
		conn.CloseNow()
	}()

	go func() {
		for {
			select {
			case blob := <-sess.out:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				err := conn.Write(ctx, websocket.MessageBinary, blob)
				cancel()
				if err != nil {
					conn.Close(websocket.StatusInternalError, "write failed")
					return
				}
			case <-sess.closed:
				return
			}
		}
	}()

	if err := s.runSession(r.Context(), conn, sess, label); err != nil {
		log.Printf("session %s: %v", sess.deviceID, err)
	}
}

// runSession speaks the frame protocol until the device disconnects.
func (s *Server) runSession(ctx context.Context, conn *websocket.Conn, sess *session, label string) error {
	hello, err := readFrame(ctx, conn)
	if err != nil {
		return err
	}
	req := hello.GetHelloRequest()
	if req == nil || req.GetDeviceId() == "" {
		return writeFrame(ctx, conn, &pb.Frame{Body: &pb.Frame_HelloResponse{
			HelloResponse: &pb.HelloResponse{Error: "hello required"},
		}})
	}
	sess.deviceID = req.GetDeviceId()

	now := time.Now().Unix()
	oldest, err := s.store.OldestEventSeq(sess.library)
	if err != nil {
		return err
	}
	snapshotRequired := req.GetLastSeq() < oldest-1 || req.GetLastSeq() <= 0

	if err := s.store.TouchDevice(sess.library, sess.deviceID, req.GetDeviceName(), req.GetAppVersion(), req.GetLastSeq(), now); err != nil {
		return err
	}

	if err := writeFrame(ctx, conn, &pb.Frame{Body: &pb.Frame_HelloResponse{
		HelloResponse: &pb.HelloResponse{
			LibraryLabel:     label,
			ServerTimeMs:     time.Now().UnixMilli(),
			SnapshotRequired: snapshotRequired,
		},
	}}); err != nil {
		return err
	}
	position := req.GetLastSeq()
	if snapshotRequired {
		// The client asks for the snapshot after hello; consuming the request
		// here keeps the live phase from reading it as a stray frame.
		request, err := readFrame(ctx, conn)
		if err != nil {
			return err
		}
		if _, ok := request.GetBody().(*pb.Frame_SnapshotRequest); !ok {
			return fmt.Errorf("expected snapshot request")
		}
		position, err = s.streamSnapshot(ctx, conn, sess)
		if err != nil {
			return err
		}
	} else {
		var err error
		position, err = s.streamLog(ctx, conn, sess, req.GetLastSeq())
		if err != nil {
			return err
		}
	}

	// The session joins the hub only once catch-up finished, so live events
	// never interleave with the snapshot or the catch-up stream. Events
	// committed in the gap are picked up by replaying from the position the
	// catch-up ended on; duplicates fan-out may already have delivered are
	// harmless, clients resolve by clock.
	s.join(sess)
	defer s.leave(sess)
	if _, err := s.streamLog(ctx, conn, sess, position); err != nil {
		return err
	}
	// Live phase: apply inbound batches, stream committed events out.
	apply := make(chan *pb.ChangeBatch, 16)
	readerDone := make(chan error, 1)
	go func() {
		for {
			frame, err := readFrame(ctx, conn)
			if err != nil {
				readerDone <- err
				return
			}
			switch body := frame.GetBody().(type) {
			case *pb.Frame_Ping:
				pong := &pb.Frame{Body: &pb.Frame_Pong{Pong: &pb.Pong{WallMs: body.Ping.GetWallMs()}}}
				if err := writeFrame(ctx, conn, pong); err != nil {
					readerDone <- err
					return
				}
			case *pb.Frame_ChangeBatch:
				select {
				case apply <- body.ChangeBatch:
				case <-sess.closed:
					readerDone <- nil
					return
				}
			case *pb.Frame_HelloRequest, *pb.Frame_SnapshotRequest:
				readerDone <- fmt.Errorf("unexpected frame")
				return
			default:
				readerDone <- fmt.Errorf("unexpected frame")
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sess.closed:
			return nil
		case err := <-readerDone:
			return err
		case batch := <-apply:
			if err := s.applyBatch(sess, batch); err != nil {
				return err
			}
		}
	}
}

func (s *Server) applyBatch(sess *session, batch *pb.ChangeBatch) error {
	ack := &pb.Ack{}
	var events []*pb.CommittedChange

	for _, ch := range batch.GetChanges() {
		if ch.GetKey() == nil || ch.GetModified() == nil {
			// A change without identity or time is malformed; refusing it
			// keeps the ack honest instead of acknowledging a drop.
			return fmt.Errorf("change without key or clock")
		}
		res, err := s.engine.Apply(sess.library, ch)
		if err != nil {
			return err
		}
		if res.Committed != nil {
			events = append(events, res.Committed)
		}
		ack.Items = append(ack.Items, &pb.Ack_Item{
			Key:      ch.GetKey(),
			Modified: ch.GetModified(),
			Applied:  res.Applied,
		})
	}

	// The events of one batch reach the other devices together.
	if len(events) > 0 {
		s.fanout(sess, events)
	}
	// The ack reaches the sender alone.
	sess.send(mustFrame(&pb.Frame{Body: &pb.Frame_Ack{Ack: ack}}))
	return nil
}

// streamSnapshot sends the full current state of the library and returns the
// sequence number it was taken at.
func (s *Server) streamSnapshot(ctx context.Context, conn *websocket.Conn, sess *session) (int64, error) {
	records, latest, err := s.store.Snapshot(sess.library)
	if err != nil {
		return 0, err
	}
	resp := &pb.Frame{Body: &pb.Frame_SnapshotResponse{
		SnapshotResponse: &pb.SnapshotResponse{Records: records, LatestSeq: latest},
	}}
	if err := writeFrame(ctx, conn, resp); err != nil {
		return 0, err
	}
	return latest, nil
}

// streamLog sends the committed changes after lastSeq, in order, and returns
// the sequence number the stream ended on.
func (s *Server) streamLog(ctx context.Context, conn *websocket.Conn, sess *session, lastSeq int64) (int64, error) {
	const pageSize = 512
	for {
		events, err := s.store.EventsSince(sess.library, lastSeq, pageSize)
		if err != nil {
			return 0, err
		}
		if len(events) == 0 {
			return lastSeq, nil
		}
		frame := &pb.Frame{Body: &pb.Frame_Events{Events: &pb.Events{Events: events}}}
		if err := writeFrame(ctx, conn, frame); err != nil {
			return 0, err
		}
		lastSeq = events[len(events)-1].Seq
		if len(events) < pageSize {
			return lastSeq, nil
		}
	}
}

func (s *Server) join(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hub := s.hubs[sess.library]
	if hub == nil {
		hub = map[*session]struct{}{}
		s.hubs[sess.library] = hub
	}
	hub[sess] = struct{}{}
}

// leave removes a session from its library hub. It is idempotent: the
// session-phase defer and the connection defer may both run it while a
// panic unwinds.
func (s *Server) leave(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hub := s.hubs[sess.library]
	if hub == nil {
		return
	}
	if _, ok := hub[sess]; !ok {
		return
	}
	delete(hub, sess)
	if len(hub) == 0 {
		delete(s.hubs, sess.library)
	}
	sess.shutdown()
}

// fanout delivers committed events to every live session of the library
// except the sender.
func (s *Server) fanout(sender *session, events []*pb.CommittedChange) {
	blob := mustFrame(&pb.Frame{Body: &pb.Frame_Events{Events: &pb.Events{Events: events}}})

	s.mu.Lock()
	defer s.mu.Unlock()
	for sess := range s.hubs[sender.library] {
		if sess == sender {
			continue
		}
		if !sess.send(blob) {
			// A dead or stuck session is dropped; it will reconnect and
			// catch up from its stored position.
			go func(drop *session) { drop.shutdown() }(sess)
		}
	}
}

func readFrame(ctx context.Context, conn *websocket.Conn) (*pb.Frame, error) {
	typ, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageBinary {
		return nil, fmt.Errorf("binary frames only")
	}
	var frame pb.Frame
	if err := proto.Unmarshal(data, &frame); err != nil {
		return nil, fmt.Errorf("decode frame: %w", err)
	}
	return &frame, nil
}

func writeFrame(ctx context.Context, conn *websocket.Conn, frame *pb.Frame) error {
	blob, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, blob)
}

func mustFrame(frame *pb.Frame) []byte {
	blob, err := proto.Marshal(frame)
	if err != nil {
		panic(fmt.Sprintf("frame is never too large to marshal: %v", err))
	}
	return blob
}
