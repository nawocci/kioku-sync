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

func TestValidateAcceptsOnlyMatchingStates(t *testing.T) {
	key := &pb.RecordKey{Kind: &pb.RecordKey_Manga{Manga: &pb.MangaKey{SourceId: 1, Url: "a"}}}
	sourceKey := &pb.RecordKey{Kind: &pb.RecordKey_SourcePreference{
		SourcePreference: &pb.SourcePrefKey{Source: "s", Pref: "p"},
	}}
	clock := &pb.Hlc{WallMs: 1, Device: "d"}
	manga := &pb.Change_Manga{Manga: &pb.MangaState{Title: "t"}}
	chapter := &pb.Change_Chapter{Chapter: &pb.ChapterState{Read: true}}
	pref := &pb.Change_Preference{Preference: &pb.PrefValue{}}

	cases := []struct {
		name string
		ch   *pb.Change
	}{
		{"matching state", &pb.Change{Key: key, Modified: clock, State: manga}},
		{"tombstone without state", &pb.Change{Key: key, Modified: clock, Tombstone: true}},
		{"preference state on a source preference key", &pb.Change{Key: sourceKey, Modified: clock, State: pref}},
		{"state that does not match the key", &pb.Change{Key: key, Modified: clock, State: chapter}},
		{"missing clock", &pb.Change{Key: key, State: manga}},
		{"missing key", &pb.Change{Modified: clock, State: manga}},
		{"empty key", &pb.Change{Key: &pb.RecordKey{}, Modified: clock, State: manga}},
		{"tombstone carrying a state", &pb.Change{Key: key, Modified: clock, Tombstone: true, State: manga}},
		{"neither state nor tombstone", &pb.Change{Key: key, Modified: clock}},
	}

	wantValid := map[string]bool{
		"matching state":                              true,
		"tombstone without state":                     true,
		"preference state on a source preference key": true,
	}
	for _, c := range cases {
		err := Validate(c.ch)
		if valid := err == nil; valid != wantValid[c.name] {
			t.Errorf("%s: valid = %v (err %v), want %v", c.name, valid, err, wantValid[c.name])
		}
	}
}
