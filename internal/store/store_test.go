package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/nawocci/kioku-sync/internal/model"
	pb "github.com/nawocci/kioku-sync/internal/pb"
	"google.golang.org/protobuf/proto"
)

func openStore(t *testing.T) (*Store, int64) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	res, err := st.DB().Exec(
		`INSERT INTO libraries (label, key_hash, created_at, last_seq) VALUES ('test', 'hash', 0, 0)`,
	)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return st, lib
}

func commitManga(st *Store, lib int64, url string, wall int64) error {
	key := &pb.RecordKey{Kind: &pb.RecordKey_Manga{
		Manga: &pb.MangaKey{SourceId: 1, Url: url},
	}}
	ch := &pb.Change{
		Key:      key,
		Modified: &pb.Hlc{WallMs: wall, Device: "d1"},
		State:    &pb.Change_Manga{Manga: &pb.MangaState{Title: url}},
	}
	blob, err := proto.Marshal(ch)
	if err != nil {
		return err
	}
	_, _, err = st.CommitRecord(
		lib, model.CanonKey(key), int(model.Manga), blob, ch.Modified,
		false, model.Summarize(ch), 0,
	)
	return err
}

// The position a snapshot reports must never sit beyond a record it left out:
// a device that stored that position would skip the missing change forever.
func TestSnapshotPositionNeverSkipsAConcurrentCommit(t *testing.T) {
	st, lib := openStore(t)

	stop := make(chan struct{})
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for wall := int64(1); ; wall++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := commitManga(st, lib, fmt.Sprintf("m%d", wall), wall); err != nil {
				select {
				case errs <- err:
				default:
				}
				return
			}
		}
	}()
	defer func() {
		close(stop)
		wg.Wait()
	}()

	for range 200 {
		snapshot, latest, err := st.Snapshot(lib)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-errs:
			if err != nil {
				t.Fatal(err)
			}
		default:
		}

		inSnapshot := make(map[string]bool, len(snapshot))
		for _, ch := range snapshot {
			inSnapshot[string(model.CanonKey(ch.GetKey()))] = true
		}

		rows, err := st.DB().Query(
			`SELECT key FROM records WHERE library_id = ? AND seq <= ?`, lib, latest,
		)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var key []byte
			if err := rows.Scan(&key); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if !inSnapshot[string(key)] {
				rows.Close()
				t.Fatalf("snapshot at position %d omits a record committed at or before it", latest)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
	}
}

// A snapshot taken between two commits reports the position it read, so the
// later commit is replayed from the log rather than skipped.
func TestSnapshotPositionIsReplayable(t *testing.T) {
	st, lib := openStore(t)

	if err := commitManga(st, lib, "a", 1); err != nil {
		t.Fatal(err)
	}
	_, latest, err := st.Snapshot(lib)
	if err != nil {
		t.Fatal(err)
	}
	if latest != 1 {
		t.Fatalf("snapshot position = %d, want 1", latest)
	}

	if err := commitManga(st, lib, "b", 2); err != nil {
		t.Fatal(err)
	}
	events, err := st.EventsSince(lib, latest, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Seq != 2 {
		t.Fatalf("replay delivered %v, want only seq 2", events)
	}
}
