// Package model holds the sync domain types shared by the storage, merge
// engine, and server: record classification, canonical key encoding, and
// human-readable summaries for the event log.
package model

import (
	"fmt"

	pb "github.com/nawocci/kioku-sync/internal/pb"
	"google.golang.org/protobuf/proto"
)

// RecordType classifies a record by its key kind. The zero value is invalid;
// every key carried on the wire has exactly one kind set.
type RecordType int

const (
	Manga RecordType = iota + 1
	Chapter
	History
	Category
	Track
	Preference
	SourcePreference
	ExtensionStore
	SourceName
)

// String returns the type name used in storage and the dashboard.
func (t RecordType) String() string {
	switch t {
	case Manga:
		return "manga"
	case Chapter:
		return "chapter"
	case History:
		return "history"
	case Category:
		return "category"
	case Track:
		return "track"
	case Preference:
		return "preference"
	case SourcePreference:
		return "source_preference"
	case ExtensionStore:
		return "extension_store"
	case SourceName:
		return "source_name"
	default:
		return "unknown"
	}
}

// TypeOf classifies a change by its key and state.
func TypeOf(ch *pb.Change) RecordType {
	switch k := ch.GetKey().GetKind().(type) {
	case *pb.RecordKey_Manga:
		return Manga
	case *pb.RecordKey_Chapter:
		return Chapter
	case *pb.RecordKey_History:
		return History
	case *pb.RecordKey_Category:
		return Category
	case *pb.RecordKey_Track:
		return Track
	case *pb.RecordKey_Preference:
		return Preference
	case *pb.RecordKey_SourcePreference:
		return SourcePreference
	case *pb.RecordKey_ExtensionStore:
		return ExtensionStore
	case *pb.RecordKey_SourceName:
		_ = k
		return SourceName
	default:
		return 0
	}
}

// Validate reports whether a change is well formed: it must carry a record key
// and a clock, its key kind must match the state it carries, and a tombstone
// must carry no state. The server refuses malformed changes at the session
// edge so a buggy or hostile client cannot store a state under a key that does
// not describe it.
func Validate(ch *pb.Change) error {
	if ch.GetKey() == nil || ch.GetModified() == nil {
		return fmt.Errorf("change without key or clock")
	}
	typ := TypeOf(ch)
	if typ == 0 {
		return fmt.Errorf("unknown record key kind")
	}
	if ch.GetTombstone() {
		if ch.GetState() != nil {
			return fmt.Errorf("tombstone carries a state")
		}
		return nil
	}
	matches := false
	switch ch.GetState().(type) {
	case *pb.Change_Manga:
		matches = typ == Manga
	case *pb.Change_Chapter:
		matches = typ == Chapter
	case *pb.Change_History:
		matches = typ == History
	case *pb.Change_Category:
		matches = typ == Category
	case *pb.Change_Track:
		matches = typ == Track
	case *pb.Change_Preference:
		// One state message serves both preference key kinds.
		matches = typ == Preference || typ == SourcePreference
	case *pb.Change_ExtensionStore:
		matches = typ == ExtensionStore
	case *pb.Change_SourceName:
		matches = typ == SourceName
	default:
		return fmt.Errorf("%s change carries no state and is not a tombstone", typ)
	}
	if !matches {
		return fmt.Errorf("%s change carries a mismatched state", typ)
	}
	return nil
}

// CanonKey returns the canonical encoded bytes of a key message. The encoding
// is deterministic, so all devices and the server agree on record identity.
func CanonKey(key *pb.RecordKey) []byte {
	// proto.Marshal is deterministic for messages without maps.
	b, err := proto.Marshal(key)
	if err != nil {
		panic(fmt.Sprintf("record key is never too large to marshal: %v", err))
	}
	return b
}

// Summarize renders a short label for the event log. It names the record in
// user terms where the payload carries something readable.
func Summarize(ch *pb.Change) string {
	key := ch.GetKey()
	if ch.GetTombstone() {
		return summarizeKey(key) + " (deleted)"
	}
	switch st := ch.GetState().(type) {
	case *pb.Change_Manga:
		if title := st.Manga.GetTitle(); title != "" {
			return "manga " + title
		}
	case *pb.Change_Category:
		if name := st.Category.GetName(); name != "" {
			return "category " + name
		}
	case *pb.Change_Track:
		if title := st.Track.GetTitle(); title != "" {
			return "track " + title
		}
	case *pb.Change_SourceName:
		if name := st.SourceName.GetName(); name != "" {
			return "source " + name
		}
	case *pb.Change_ExtensionStore:
		if name := st.ExtensionStore.GetName(); name != "" {
			return "store " + name
		}
	}
	return summarizeKey(key)
}

func summarizeKey(key *pb.RecordKey) string {
	switch k := key.GetKind().(type) {
	case *pb.RecordKey_Manga:
		return fmt.Sprintf("manga %d %s", k.Manga.GetSourceId(), k.Manga.GetUrl())
	case *pb.RecordKey_Chapter:
		return fmt.Sprintf("chapter %s", k.Chapter.GetUrl())
	case *pb.RecordKey_History:
		return fmt.Sprintf("history %s", k.History.GetChapterUrl())
	case *pb.RecordKey_Category:
		return fmt.Sprintf("category %s", k.Category.GetId())
	case *pb.RecordKey_Track:
		return fmt.Sprintf("track %d", k.Track.GetTrackerId())
	case *pb.RecordKey_Preference:
		return fmt.Sprintf("preference %s", k.Preference)
	case *pb.RecordKey_SourcePreference:
		return fmt.Sprintf("source preference %s/%s", k.SourcePreference.GetSource(), k.SourcePreference.GetPref())
	case *pb.RecordKey_ExtensionStore:
		return fmt.Sprintf("store %s", k.ExtensionStore)
	case *pb.RecordKey_SourceName:
		return fmt.Sprintf("source %d", k.SourceName)
	default:
		return "unknown record"
	}
}
