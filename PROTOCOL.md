# Kioku Sync protocol

The client (Kioku, the Android app) and the server (kioku-sync) speak one
protocol over a WebSocket. This document is the contract; the
`proto/sync/v1/sync.proto` file is its wire format.

## Transport

- `GET /ws`, binary frames only, each carrying one protobuf `Frame`.
- Authentication: `Authorization: Bearer <kioku_…>` — one key identifies one
  library, and every device of that library shares the key.
- The WebSocket upgrade request and its 101 response both carry the header
  `X-Kioku-Sync-Protocol: <version>`.

## Versioning

The protocol version is independent of the application version. It changes
only when the wire format or the semantics of an existing message change —
never for server-internal fixes. The current version is **2**; it added the
snapshot size to the hello, paged the snapshot, and dropped chapter metadata
from the wire.

The check is strict in both directions, before any protobuf frame is
exchanged:

- The server rejects an upgrade whose header is missing or names a different
  version, with an explanation in the HTTP response body.
- The client refuses a session when the 101 response does not name its own
  version, so an older server cannot be spoken to silently.

A deployment must therefore upgrade the server and all devices together
whenever the version changes: an old client is refused by the new server,
and a new client refuses an old server.

## Records

Every synced value is a *record* identified by a `RecordKey` whose canonical
protobuf encoding is its storage identity on the server and on every client.
Nine kinds exist: manga, chapter, history, category, track, preference,
source preference, extension store, and source name.

History is a record distinct from the chapter it belongs to, so a chapter's
user state and its reading history are merged independently.

What each record carries is deliberate:

- A manga record carries the user's state (favorite time, notes, reader and
  chapter flags, categories) *and* a metadata snapshot, so a device that has
  just paired can present the library before its sources refresh. The source
  stays authoritative for that metadata afterwards.
- A chapter record carries user state only — read, bookmark, last page read.
  A chapter's name, number, scanlator and dates come back from the source on
  every refresh, so syncing them turned a routine refresh into thousands of
  records' worth of traffic. A device that has not fetched the chapter yet
  creates a placeholder and lets the next refresh fill it in.
- A history record carries the read time and the reading duration.

## Ordering and conflict resolution

Every change carries a hybrid logical clock value (`Hlc`): wall milliseconds,
a logical counter, and the device id as the final deterministic tiebreak.

- The server resolves concurrent pushes last-write-wins on that ordering,
  atomically: a committed record can never be overwritten by an older value.
- History records are the one exception: their fields union (latest `read_at`
  and longest `read_duration` survive) so reads on concurrent devices are all
  kept. History is therefore append-only in practice: clearing a read locally
  does not erase it elsewhere. Only a tombstone removes history, and that is
  what a deleted chapter or library entry sends.
- Clients apply a server-committed change only when its clock is newer than
  the one they last knew for the record.

## Session flow

1. Client opens the socket and sends `HelloRequest` (device id, name, app
   version, last seen sequence number).
2. Server replies `HelloResponse`. When the client's sequence is behind the
   retained event log (or zero, on first pairing) the response demands a
   snapshot and reports how many records it will carry, so a device pairing
   into a non-empty library can ask about the merge before anything is
   transferred. Otherwise the server streams the missed events.
3. Snapshot: the client asks for it, and the server streams every record
   including tombstones in pages of up to 500, marking the final page. A page
   is applied as it arrives, so a large library never has to fit in one frame
   or in memory at once, and a device joining late sees server-side deletions
   instead of resurrecting records.
4. Live phase: the client pushes `ChangeBatch` frames; the server commits
   what wins, answers with an `Ack` (per change, whether it was applied),
   and fans the committed changes out to the other devices of the library.

A change that lost at the server is never distributed: the losing device
learns the winning state from the matching event.

## Deletions

A deletion is a change with `tombstone = true` and no state. Tombstones
persist like any record and are included in snapshots, so a deletion
propagates to every device eventually, including ones that were offline when
it happened. The event log is retained for 30 days; a device that falls
behind that window is served a snapshot instead, so retention never loses a
change.
