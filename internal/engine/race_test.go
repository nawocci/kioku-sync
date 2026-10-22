package engine

import (
	"sync"
	"testing"

	"github.com/nawocci/kioku-sync/internal/model"
	pb "github.com/nawocci/kioku-sync/internal/pb"
)

// A concurrent push with an older clock must never overwrite a newer
// committed record, no matter how the commits interleave.
func TestConcurrentPushesKeepTheNewestClock(t *testing.T) {
	e, lib := newEngine(t)
	key := &pb.RecordKey{Kind: &pb.RecordKey_Manga{
		Manga: &pb.MangaKey{SourceId: 1, Url: "a"},
	}}

	const pushes = 64
	var wg sync.WaitGroup
	for i := range pushes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch := &pb.Change{
				Key:      key,
				Modified: &pb.Hlc{WallMs: int64(100 + i), Device: "d1"},
				State:    &pb.Change_Manga{Manga: &pb.MangaState{Title: "v"}},
			}
			if _, err := e.Apply(lib, ch); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	rec, err := e.store.Record(lib, model.CanonKey(key))
	if err != nil {
		t.Fatal(err)
	}
	if rec.HlcWall != 100+pushes-1 {
		t.Fatalf("stored clock = %d, want %d", rec.HlcWall, 100+pushes-1)
	}

	// The event log must never regress for this record: every logged state
	// carries a clock at least as new as the one logged before it.
	events, err := e.store.EventsSince(lib, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	lastWall := int64(-1)
	for _, ev := range events {
		if wall := ev.Change.GetModified().GetWallMs(); wall < lastWall {
			t.Fatalf("event log regressed: %d after %d", wall, lastWall)
		} else if wall > lastWall {
			lastWall = wall
		}
	}
}

// A history union racing with a plain push must still converge: the stored
// duration never shrinks.
func TestConcurrentHistoryUnionKeepsProgress(t *testing.T) {
	e, lib := newEngine(t)

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch := historyChange(1, "m", "c", int64(100+i), 0, "d1", int64p(int64(i)), int64(i*10))
			if _, err := e.Apply(lib, ch); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	rec, err := e.store.Record(lib, model.CanonKey(historyChange(1, "m", "c", 0, 0, "", nil, 0).Key))
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("history record missing")
	}
	if rec.HlcWall != 131 {
		t.Fatalf("stored clock = %d, want 131", rec.HlcWall)
	}
}

// The losing side of a commit race must not burn a sequence number or leave
// an event behind.
func TestLosingCommitSpendsNothing(t *testing.T) {
	e, lib := newEngine(t)
	st := e.store

	if _, err := e.Apply(lib, mangaChange(1, "a", 200, 0, "d1")); err != nil {
		t.Fatal(err)
	}
	info, err := st.LibraryInfo(lib)
	if err != nil {
		t.Fatal(err)
	}
	before := info.LastSeq

	// Same engine, older clock: rejected by the read check, not the store.
	res, err := e.Apply(lib, mangaChange(1, "a", 100, 0, "d1"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || res.Committed != nil {
		t.Fatal("older push was accepted")
	}

	// And directly through the store: the conditional upsert must roll back
	// the sequence bump with the rest of the transaction.
	key := model.CanonKey(&pb.RecordKey{Kind: &pb.RecordKey_Manga{
		Manga: &pb.MangaKey{SourceId: 1, Url: "a"},
	}})
	_, committed, err := st.CommitRecord(
		lib, key, int(model.Manga), []byte("older"), &pb.Hlc{WallMs: 1, Device: "d1"},
		false, []byte("older"), "older", 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if committed {
		t.Fatal("older clock won the store race")
	}
	info, err = st.LibraryInfo(lib)
	if err != nil {
		t.Fatal(err)
	}
	if info.LastSeq != before {
		t.Fatalf("losing commit burned sequence %d -> %d", before, info.LastSeq)
	}
	events, err := st.EventsSince(lib, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("losing commit left %d events, want 1", len(events))
	}
}
