package model

import (
	"testing"

	pb "github.com/nawocci/kioku-sync/internal/pb"
)

func TestCanonKeyIsDeterministic(t *testing.T) {
	build := func() *pb.RecordKey {
		return &pb.RecordKey{Kind: &pb.RecordKey_Chapter{
			Chapter: &pb.ChapterKey{
				Manga: &pb.MangaKey{SourceId: 42, Url: "https://example.com/manga"},
				Url:   "c",
			},
		}}
	}
	a, b := CanonKey(build()), CanonKey(build())
	if string(a) != string(b) {
		t.Fatal("the same key must always encode to the same bytes")
	}
}

func TestDistinctKindsNeverCollide(t *testing.T) {
	manga := &pb.MangaKey{SourceId: 1, Url: "u"}
	keys := []*pb.RecordKey{
		{Kind: &pb.RecordKey_Chapter{Chapter: &pb.ChapterKey{Manga: manga, Url: "c"}}},
		{Kind: &pb.RecordKey_History{History: &pb.HistoryKey{Manga: manga, ChapterUrl: "c"}}},
		{Kind: &pb.RecordKey_Preference{Preference: "c"}},
		{Kind: &pb.RecordKey_SourceName{SourceName: 1}},
	}
	seen := map[string]RecordType{}
	for _, k := range keys {
		seen[string(CanonKey(k))] = TypeOf(&pb.Change{Key: k})
	}
	if len(seen) != len(keys) {
		t.Fatalf("%d distinct keys must not share canonical bytes", len(keys))
	}
}

func TestTypeOfRejectsUnknownKeys(t *testing.T) {
	if got := TypeOf(&pb.Change{Key: &pb.RecordKey{}}); got != 0 {
		t.Fatalf("empty key classified as %d, want 0", got)
	}
}
