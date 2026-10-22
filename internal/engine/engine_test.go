package engine

import (
	"path/filepath"
	"testing"

	"github.com/nawocci/kioku-sync/internal/hlc"
	pb "github.com/nawocci/kioku-sync/internal/pb"
	"github.com/nawocci/kioku-sync/internal/store"
)

func mangaChange(source int64, url string, wall int64, counter uint32, device string) *pb.Change {
	return &pb.Change{
		Key: &pb.RecordKey{Kind: &pb.RecordKey_Manga{
			Manga: &pb.MangaKey{SourceId: source, Url: url},
		}},
		Modified: &pb.Hlc{WallMs: wall, Counter: counter, Device: device},
		State: &pb.Change_Manga{
			Manga: &pb.MangaState{Title: "m" + url, ReaderFlags: 1},
		},
	}
}

func historyChange(source int64, mangaURL, chapterURL string, wall int64, counter uint32,
	device string, readAt *int64, duration int64,
) *pb.Change {
	return &pb.Change{
		Key: &pb.RecordKey{Kind: &pb.RecordKey_History{
			History: &pb.HistoryKey{
				Manga:      &pb.MangaKey{SourceId: source, Url: mangaURL},
				ChapterUrl: chapterURL,
			},
		}},
		Modified: &pb.Hlc{WallMs: wall, Counter: counter, Device: device},
		State: &pb.Change_History{
			History: &pb.HistoryState{ReadAt: readAt, ReadDuration: duration},
		},
	}
}

func int64p(v int64) *int64 { return &v }

func newEngine(t *testing.T) (*Engine, int64) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.DB().Exec(
		`INSERT INTO libraries (label, key_hash, created_at, last_seq) VALUES ('test', 'hash', 0, 0)`,
	); err != nil {
		t.Fatal(err)
	}
	return New(st), 1
}

func TestFreshRecordLands(t *testing.T) {
	e, lib := newEngine(t)
	res, err := e.Apply(lib, mangaChange(1, "a", 100, 0, "d1"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied || res.Committed == nil || res.Committed.Seq != 1 {
		t.Fatalf("fresh record not committed: %+v", res)
	}
}

func TestLwwNewerWins(t *testing.T) {
	e, lib := newEngine(t)
	if _, err := e.Apply(lib, mangaChange(1, "a", 100, 0, "d1")); err != nil {
		t.Fatal(err)
	}
	// Same wall, higher counter from another device: counter breaks the tie.
	res, err := e.Apply(lib, mangaChange(1, "a", 100, 1, "d2"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied || res.Committed == nil {
		t.Fatal("newer change not applied")
	}
	// Older push loses.
	res, err = e.Apply(lib, mangaChange(1, "a", 100, 0, "d3"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied {
		t.Fatal("older change was applied")
	}
}

func TestTombstoneBeatsOlderAndLosesToNewer(t *testing.T) {
	e, lib := newEngine(t)
	if _, err := e.Apply(lib, mangaChange(1, "a", 100, 0, "d1")); err != nil {
		t.Fatal(err)
	}

	tomb := mangaChange(1, "a", 200, 0, "d1")
	tomb.Tombstone = true
	tomb.State = nil
	res, err := e.Apply(lib, tomb)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Committed.Change.GetTombstone() {
		t.Fatal("tombstone not committed")
	}

	// A delayed push older than the tombstone must not resurrect the record.
	res, err = e.Apply(lib, mangaChange(1, "a", 150, 0, "d2"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied {
		t.Fatal("older change resurrected a tombstoned record")
	}

	// A genuinely newer change recreates the record.
	res, err = e.Apply(lib, mangaChange(1, "a", 300, 0, "d2"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied || res.Committed.Change.GetTombstone() {
		t.Fatal("newer change did not recreate the record")
	}
}

func TestHistoryUnions(t *testing.T) {
	e, lib := newEngine(t)
	if _, err := e.Apply(lib, historyChange(1, "a", "c1", 100, 0, "d1", int64p(1000), 500)); err != nil {
		t.Fatal(err)
	}
	// The other device read the same chapter earlier but for longer.
	res, err := e.Apply(lib, historyChange(1, "a", "c1", 200, 0, "d2", int64p(900), 900))
	if err != nil {
		t.Fatal(err)
	}
	h := res.Committed.Change.GetHistory()
	if h.ReadAt == nil || *h.ReadAt != 1000 {
		t.Fatalf("read_at union = %v, want 1000", h.ReadAt)
	}
	if h.GetReadDuration() != 900 {
		t.Fatalf("duration union = %d, want 900", h.GetReadDuration())
	}

	// The stored clock is the later of the two, so a replay of the first
	// push is rejected instead of undoing the union.
	replay, err := e.Apply(lib, historyChange(1, "a", "c1", 100, 0, "d1", int64p(1000), 500))
	if err != nil {
		t.Fatal(err)
	}
	if replay.Applied {
		t.Fatal("stale replay after union was applied")
	}
}

func TestIdenticalClockIsANoop(t *testing.T) {
	e, lib := newEngine(t)
	first, err := e.Apply(lib, mangaChange(1, "a", 100, 0, "d1"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.Apply(lib, mangaChange(1, "a", 100, 0, "d1"))
	if err != nil {
		t.Fatal(err)
	}
	if again.Applied && again.Committed != nil {
		t.Fatal("identical change produced an event")
	}
	if again.Committed != nil && again.Committed.Seq != first.Committed.Seq {
		t.Fatalf("identical change bumped the sequence")
	}
}

func TestSnapshotIncludesTombstones(t *testing.T) {
	e, lib := newEngine(t)
	st := e.store

	if _, err := e.Apply(lib, mangaChange(1, "a", 100, 0, "d1")); err != nil {
		t.Fatal(err)
	}
	tomb := mangaChange(1, "a", 200, 0, "d1")
	tomb.Tombstone = true
	tomb.State = nil
	if _, err := e.Apply(lib, tomb); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(lib, mangaChange(1, "b", 100, 0, "d1")); err != nil {
		t.Fatal(err)
	}

	snap, _, err := st.Snapshot(lib)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap) != 2 {
		t.Fatalf("snapshot has %d records, want 2", len(snap))
	}
	tombstones := 0
	for _, ch := range snap {
		if ch.GetTombstone() {
			tombstones++
			if hlc.Compare(ch.GetModified(), tomb.GetModified()) != 0 {
				t.Fatalf("tombstone clock mismatch: %v", ch.GetModified())
			}
		}
	}
	if tombstones != 1 {
		t.Fatalf("snapshot carries %d tombstones, want 1", tombstones)
	}
}
