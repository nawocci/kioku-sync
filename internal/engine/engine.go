// Package engine applies incoming changes to stored records. Every record is
// resolved by last-write-wins on the hybrid logical clock, except history
// records, which union their fields so reads from concurrent devices are all
// kept. Commits are resolved atomically by the store, so concurrent sessions
// can never let an older value overwrite a newer one.
package engine

import (
	"fmt"
	"time"

	"github.com/nawocci/kioku-sync/internal/hlc"
	"github.com/nawocci/kioku-sync/internal/model"
	pb "github.com/nawocci/kioku-sync/internal/pb"
	"github.com/nawocci/kioku-sync/internal/store"
	"google.golang.org/protobuf/proto"
)

// Result reports what one applied change did.
type Result struct {
	// Committed is set when the record state changed; its Seq orders the
	// change in the library's event log and fans out to other devices.
	Committed *pb.CommittedChange
	// Applied marks that the incoming clock was accepted, even when the
	// stored value was already identical and no event was needed.
	Applied bool
}

// Engine merges changes into a store.
type Engine struct {
	store *store.Store
	now   func() time.Time
}

// New creates an engine writing to st.
func New(st *store.Store) *Engine {
	return &Engine{store: st, now: time.Now}
}

// Apply merges one incoming change into the library.
func (e *Engine) Apply(library int64, in *pb.Change) (Result, error) {
	key := model.CanonKey(in.GetKey())
	typ := model.TypeOf(in)
	if typ == 0 {
		return Result{}, fmt.Errorf("unknown record key kind")
	}

	current, err := e.store.Record(library, key)
	if err != nil {
		return Result{}, err
	}
	at := e.now().Unix()

	// A record the library has never seen simply lands.
	if current == nil {
		return e.commit(library, key, typ, in, at)
	}

	stored := &pb.Hlc{WallMs: current.HlcWall, Counter: current.HlcCount, Device: current.HlcDevice}
	switch cmp := hlc.Compare(in.GetModified(), stored); {
	case cmp < 0:
		// The server already holds a newer state; the push loses.
		return Result{Applied: false}, nil

	case cmp == 0:
		// The library already holds this exact clock, which only a retried
		// push produces. It is accepted as applied: no event is needed, since
		// every device has either seen this value or a newer one.
		return Result{Applied: true}, nil

	default:
		if in.GetTombstone() || current.Tombstone ||
			typ != model.History ||
			current.Payload == nil {
			return e.commit(library, key, typ, in, at)
		}
		// History unions: the latest read time and the longest duration of
		// both states survive, under the later of the two clocks.
		merged, err := e.mergeHistory(current, in)
		if err != nil {
			return Result{}, err
		}
		return e.commit(library, key, typ, merged, at)
	}
}

// commit stores ch as the new state of the record under ch's own clock and
// logs it as an event. When another commit for the same record won the race,
// nothing is stored and the push is reported as rejected.
func (e *Engine) commit(library int64, key []byte, typ model.RecordType, ch *pb.Change, at int64) (Result, error) {
	blob, err := encode(ch)
	if err != nil {
		return Result{}, err
	}

	seq, committed, err := e.store.CommitRecord(
		library, key, int(typ), blob, ch.GetModified(), ch.GetTombstone(),
		model.Summarize(ch), at,
	)
	if err != nil {
		return Result{}, err
	}
	if !committed {
		return Result{Applied: false}, nil
	}
	return Result{
		Committed: &pb.CommittedChange{Change: ch, Seq: seq},
		Applied:   true,
	}, nil
}

// mergeHistory unions the incoming state into the stored one and returns the
// resulting change under the later clock.
func (e *Engine) mergeHistory(current *store.Record, in *pb.Change) (*pb.Change, error) {
	var stored pb.Change
	if err := decode(current.Payload, &stored); err != nil {
		return nil, err
	}

	inH := in.GetHistory()
	storedH := stored.GetHistory()
	var readAt *int64
	switch {
	case inH.ReadAt == nil:
		readAt = storedH.ReadAt
	case storedH.ReadAt == nil:
		readAt = inH.ReadAt
	case *storedH.ReadAt > *inH.ReadAt:
		readAt = storedH.ReadAt
	default:
		readAt = inH.ReadAt
	}
	duration := max(inH.GetReadDuration(), storedH.GetReadDuration())

	key := proto.Clone(in.GetKey()).(*pb.RecordKey)
	modified := hlc.Max(in.GetModified(), &pb.Hlc{
		WallMs: current.HlcWall, Counter: current.HlcCount, Device: current.HlcDevice,
	})
	return &pb.Change{
		Key:      key,
		Modified: modified,
		State: &pb.Change_History{
			History: &pb.HistoryState{
				ReadAt:       readAt,
				ReadDuration: duration,
			},
		},
	}, nil
}

func encode(ch *pb.Change) ([]byte, error) {
	b, err := proto.Marshal(ch)
	if err != nil {
		return nil, fmt.Errorf("encode change: %w", err)
	}
	return b, nil
}

func decode(b []byte, ch *pb.Change) error {
	if err := proto.Unmarshal(b, ch); err != nil {
		return fmt.Errorf("decode change: %w", err)
	}
	return nil
}
