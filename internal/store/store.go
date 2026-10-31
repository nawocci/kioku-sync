// Package store is the SQLite persistence layer for libraries, devices,
// record states, and the per-library event log.
package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/nawocci/kioku-sync/internal/model"
	pb "github.com/nawocci/kioku-sync/internal/pb"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"
)

// ErrUnknownLibrary reports an operation against a library that does not
// exist, e.g. after a key was revoked.
var ErrUnknownLibrary = errors.New("unknown library")

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating when absent) the database at path and applies the
// schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// modernc.org/sqlite allows one writer at a time; serialize and wait
	// instead of failing when several sessions commit at once.
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = NORMAL`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB exposes the underlying database for key management.
func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS libraries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	label TEXT NOT NULL UNIQUE,
	key_hash TEXT NOT NULL UNIQUE,
	created_at INTEGER NOT NULL,
	last_seq INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS devices (
	library_id INTEGER NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
	device_id TEXT NOT NULL,
	name TEXT NOT NULL,
	app_version TEXT NOT NULL,
	last_seen INTEGER NOT NULL,
	last_seq INTEGER NOT NULL,
	PRIMARY KEY (library_id, device_id)
);

CREATE TABLE IF NOT EXISTS records (
	library_id INTEGER NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
	key BLOB NOT NULL,
	type INTEGER NOT NULL,
	payload BLOB,
	hlc_wall INTEGER NOT NULL,
	hlc_counter INTEGER NOT NULL,
	hlc_device TEXT NOT NULL,
	tombstone INTEGER NOT NULL DEFAULT 0,
	seq INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	PRIMARY KEY (library_id, key)
);

CREATE TABLE IF NOT EXISTS events (
	library_id INTEGER NOT NULL,
	seq INTEGER NOT NULL,
	type INTEGER NOT NULL,
	key BLOB NOT NULL,
	change BLOB NOT NULL,
	summary TEXT NOT NULL,
	at INTEGER NOT NULL,
	PRIMARY KEY (library_id, seq)
);
`

// Record is the stored state of one synced record.
type Record struct {
	Key       []byte
	Type      int
	Payload   []byte // nil for tombstones
	HlcWall   int64
	HlcCount  uint32
	HlcDevice string
	Tombstone bool
	Seq       int64
}

// Record fetches the current state of a record, or nil when the library has
// never seen it.
func (s *Store) Record(library int64, key []byte) (*Record, error) {
	row := s.db.QueryRow(
		`SELECT key, type, payload, hlc_wall, hlc_counter, hlc_device, tombstone, seq
		 FROM records WHERE library_id = ? AND key = ?`, library, key,
	)
	var r Record
	err := row.Scan(&r.Key, &r.Type, &r.Payload, &r.HlcWall, &r.HlcCount, &r.HlcDevice, &r.Tombstone, &r.Seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CommitRecord stores the new state of a record under the next sequence
// number of its library and appends the matching event, in one transaction.
// The record is only written when hlc is strictly newer than the stored
// clock, so concurrent commits can never let an older value overwrite a
// newer one; a change that loses the race leaves no record, event, or
// sequence number behind. payload is the full committed Change message, stored
// once and replayed from the event log; summary is its display label.
func (s *Store) CommitRecord(library int64, key []byte, typ int, payload []byte,
	hlc *pb.Hlc, tombstone bool, summary string, at int64,
) (seq int64, committed bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback() //nolint:errcheck // nothing to roll back after commit

	if err := tx.QueryRow(
		`UPDATE libraries SET last_seq = last_seq + 1 WHERE id = ? RETURNING last_seq`, library,
	).Scan(&seq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, ErrUnknownLibrary
		}
		return 0, false, err
	}

	res, err := tx.Exec(
		`INSERT INTO records (library_id, key, type, payload, hlc_wall, hlc_counter, hlc_device, tombstone, seq, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (library_id, key) DO UPDATE SET
			type = excluded.type, payload = excluded.payload,
			hlc_wall = excluded.hlc_wall, hlc_counter = excluded.hlc_counter, hlc_device = excluded.hlc_device,
			tombstone = excluded.tombstone, seq = excluded.seq, updated_at = excluded.updated_at
		 WHERE excluded.hlc_wall > records.hlc_wall
		    OR (excluded.hlc_wall = records.hlc_wall AND excluded.hlc_counter > records.hlc_counter)
		 OR (excluded.hlc_wall = records.hlc_wall AND excluded.hlc_counter = records.hlc_counter
		     AND excluded.hlc_device > records.hlc_device)`,
		library, key, typ, payload, hlc.GetWallMs(), hlc.GetCounter(), hlc.GetDevice(),
		tombstone, seq, at,
	)
	if err != nil {
		return 0, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if affected == 0 {
		// Another commit for this record carried a clock at least as new;
		// roll the transaction back so no sequence number or event is spent.
		return 0, false, nil
	}

	if _, err := tx.Exec(
		`INSERT INTO events (library_id, seq, type, key, change, summary, at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		library, seq, typ, key, payload, summary, at,
	); err != nil {
		return 0, false, err
	}
	return seq, true, tx.Commit()
}

// LatestSeq returns the newest committed sequence number of a library.
func (s *Store) LatestSeq(library int64) (int64, error) {
	var seq int64
	err := s.db.QueryRow(`SELECT last_seq FROM libraries WHERE id = ?`, library).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrUnknownLibrary
	}
	return seq, err
}

// RecordCount counts the records of a library, tombstones included.
func (s *Store) RecordCount(library int64) (int64, error) {
	var count int64
	err := s.db.QueryRow(`SELECT count(*) FROM records WHERE library_id = ?`, library).Scan(&count)
	return count, err
}

// snapshotChunk bounds how many records one internal snapshot query reads.
const snapshotChunk = 1000

// SnapshotCursor is the position of a record in a library's snapshot order.
// The zero value starts at the first record.
type SnapshotCursor struct {
	Type int
	Key  []byte
}

// SnapshotPage returns up to limit records of a library after the cursor,
// ordered by type then key, together with the cursor to continue from. The
// snapshot is paged so a large library never has to fit in one frame or in
// the server's memory at once.
func (s *Store) SnapshotPage(library int64, after SnapshotCursor, limit int) ([]*pb.Change, SnapshotCursor, error) {
	key := after.Key
	if key == nil {
		key = []byte{}
	}
	rows, err := s.db.Query(
		`SELECT type, key, payload, hlc_wall, hlc_counter, hlc_device, tombstone
		 FROM records
		 WHERE library_id = ? AND (type > ? OR (type = ? AND key > ?))
		 ORDER BY type, key
		 LIMIT ?`,
		library, after.Type, after.Type, key, limit,
	)
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()

	cursor := after
	changes := make([]*pb.Change, 0, limit)
	for rows.Next() {
		var typ int
		var recordKey, payload []byte
		var wall int64
		var counter uint32
		var device string
		var tombstone bool
		if err := rows.Scan(&typ, &recordKey, &payload, &wall, &counter, &device, &tombstone); err != nil {
			return nil, after, err
		}
		cursor = SnapshotCursor{Type: typ, Key: recordKey}

		ch := &pb.Change{
			Modified:  &pb.Hlc{WallMs: wall, Counter: counter, Device: device},
			Tombstone: tombstone,
		}
		if len(payload) > 0 {
			if err := proto.Unmarshal(payload, ch); err != nil {
				return nil, after, fmt.Errorf("decode record payload: %w", err)
			}
		}
		changes = append(changes, ch)
	}
	return changes, cursor, rows.Err()
}

// Snapshot returns the current state of every record of a library, including
// tombstones, together with the log position it corresponds to.
//
// The position is read before the records, so a commit that lands while the
// snapshot is being read stays ahead of the returned position and the client
// replays it from the log. Reading the position last would move the client's
// position past a change the snapshot never contained, and that change would be
// lost for the device.
func (s *Store) Snapshot(library int64) ([]*pb.Change, int64, error) {
	latest, err := s.LatestSeq(library)
	if err != nil {
		return nil, 0, err
	}
	var changes []*pb.Change
	var after SnapshotCursor
	for {
		page, cursor, err := s.SnapshotPage(library, after, snapshotChunk)
		if err != nil {
			return nil, 0, err
		}
		changes = append(changes, page...)
		if len(page) < snapshotChunk {
			return changes, latest, nil
		}
		after = cursor
	}
}

// OldestEventSeq returns the lowest retained sequence number of a library's
// event log.
func (s *Store) OldestEventSeq(library int64) (int64, error) {
	var seq int64
	err := s.db.QueryRow(
		`SELECT seq FROM events WHERE library_id = ? ORDER BY seq LIMIT 1`, library,
	).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		// An empty log serves any client from its own position.
		return 1, nil
	}
	return seq, err
}

// EventsSince returns the committed changes of a library after seq, up to
// limit entries, in order.
func (s *Store) EventsSince(library int64, seq int64, limit int) ([]*pb.CommittedChange, error) {
	rows, err := s.db.Query(
		`SELECT seq, change FROM events WHERE library_id = ? AND seq > ? ORDER BY seq LIMIT ?`,
		library, seq, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*pb.CommittedChange
	for rows.Next() {
		var seq int64
		var blob []byte
		if err := rows.Scan(&seq, &blob); err != nil {
			return nil, err
		}
		committed := &pb.CommittedChange{Seq: seq, Change: &pb.Change{}}
		if err := proto.Unmarshal(blob, committed.Change); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		events = append(events, committed)
	}
	return events, rows.Err()
}

// TouchDevice records a device sighting and its log position.
func (s *Store) TouchDevice(library int64, deviceID, name, appVersion string, lastSeq, at int64) error {
	_, err := s.db.Exec(
		`INSERT INTO devices (library_id, device_id, name, app_version, last_seen, last_seq)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (library_id, device_id) DO UPDATE SET
			name = excluded.name, app_version = excluded.app_version,
			last_seen = excluded.last_seen, last_seq = excluded.last_seq`,
		library, deviceID, name, appVersion, at, lastSeq,
	)
	return err
}

// Device is a registered device of a library.
type Device struct {
	ID         string
	Name       string
	AppVersion string
	LastSeen   int64
	LastSeq    int64
}

// Devices lists the devices of a library, most recently seen first.
func (s *Store) Devices(library int64) ([]Device, error) {
	rows, err := s.db.Query(
		`SELECT device_id, name, app_version, last_seen, last_seq FROM devices
		 WHERE library_id = ? ORDER BY last_seen DESC`, library,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.Name, &d.AppVersion, &d.LastSeen, &d.LastSeq); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// Counts is the per-type record census of a library.
type Counts struct {
	Type      string `json:"type"`
	Count     int    `json:"count"`
	Tombstone int    `json:"tombstone"`
}

// Counts summarizes the records of a library by type.
func (s *Store) Counts(library int64) ([]Counts, error) {
	rows, err := s.db.Query(
		`SELECT type, count(*), sum(tombstone) FROM records
		 WHERE library_id = ? GROUP BY type ORDER BY type`, library,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make([]Counts, 0)
	for rows.Next() {
		var typ, count, tombstones int
		if err := rows.Scan(&typ, &count, &tombstones); err != nil {
			return nil, err
		}
		counts = append(counts, Counts{
			Type:      model.RecordType(typ).String(),
			Count:     count,
			Tombstone: tombstones,
		})
	}
	return counts, rows.Err()
}

// EventSummary is one entry of the dashboard event feed.
type EventSummary struct {
	Seq     int64  `json:"seq"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
	At      int64  `json:"at"`
}

// RecentEvents returns the newest events of a library.
func (s *Store) RecentEvents(library int64, limit int) ([]EventSummary, error) {
	rows, err := s.db.Query(
		`SELECT seq, type, summary, at FROM events
		 WHERE library_id = ? ORDER BY seq DESC LIMIT ?`, library, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]EventSummary, 0)
	for rows.Next() {
		var e EventSummary
		var typ int
		if err := rows.Scan(&e.Seq, &typ, &e.Summary, &e.At); err != nil {
			return nil, err
		}
		e.Type = model.RecordType(typ).String()
		events = append(events, e)
	}
	return events, rows.Err()
}

// LibraryInfo describes a library for the dashboard.
type LibraryInfo struct {
	Label     string
	CreatedAt int64
	LastSeq   int64
}

// LibraryInfo fetches the identity of a library.
func (s *Store) LibraryInfo(library int64) (LibraryInfo, error) {
	var info LibraryInfo
	err := s.db.QueryRow(
		`SELECT label, created_at, last_seq FROM libraries WHERE id = ?`, library,
	).Scan(&info.Label, &info.CreatedAt, &info.LastSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return info, ErrUnknownLibrary
	}
	return info, err
}

// PruneEvents drops events older than cutoff. A device whose position falls
// below the retained log is served a snapshot instead, so no change is lost.
func (s *Store) PruneEvents(cutoff int64) error {
	_, err := s.db.Exec(`DELETE FROM events WHERE at < ?`, cutoff)
	return err
}
