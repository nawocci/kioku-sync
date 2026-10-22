package keys

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE libraries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		label TEXT NOT NULL UNIQUE,
		key_hash TEXT NOT NULL UNIQUE,
		created_at INTEGER NOT NULL,
		last_seq INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCreateKeyRoundTrip(t *testing.T) {
	db := newDB(t)
	key, err := CreateKey(db, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if len(key) <= len(KeyPrefix)+32 {
		t.Fatalf("key too short: %q", key)
	}
	id, label, err := Lookup(db, key)
	if err != nil || id == 0 || label != "mine" {
		t.Fatalf("lookup = %d %q %v", id, label, err)
	}
}

func TestCreateKeyDefaultsLabelsInSequence(t *testing.T) {
	db := newDB(t)
	for _, want := range []string{"Key-1", "Key-2", "Key-3"} {
		key, err := CreateKey(db, "")
		if err != nil {
			t.Fatal(err)
		}
		_, label, err := Lookup(db, key)
		if err != nil {
			t.Fatal(err)
		}
		if label != want {
			t.Fatalf("label = %q, want %q", label, want)
		}
	}
	// An explicit label still lands verbatim.
	if _, err := CreateKey(db, "tablet"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateKeyRejectsDuplicateLabel(t *testing.T) {
	db := newDB(t)
	if _, err := CreateKey(db, "dup"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateKey(db, "dup"); err != ErrDuplicateLabel {
		t.Fatalf("duplicate = %v, want ErrDuplicateLabel", err)
	}
}

func TestLookupUnknownKey(t *testing.T) {
	db := newDB(t)
	id, _, err := Lookup(db, "kioku_unknown")
	if err != nil || id != 0 {
		t.Fatalf("lookup = %d %v, want 0 nil", id, err)
	}
}

func TestHashIsStable(t *testing.T) {
	if Hash("a") != Hash("a") {
		t.Fatal("hash not deterministic")
	}
	if Hash("a") == Hash("b") {
		t.Fatal("hash collision on trivial input")
	}
}
