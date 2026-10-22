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
never for server-internal fixes.

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

## Ordering and conflict resolution

Every change carries a hybrid logical clock value (`Hlc`): wall milliseconds,
a logical counter, and the device id as the final deterministic tiebreak.

- The server resolves concurrent pushes last-write-wins on that ordering,
  atomically: a committed record can never be overwritten by an older value.
- History records are the one exception: their fields union (latest `read_at`
  and longest `read_duration` survive) so reads on concurrent devices are all
  kept.
- Clients apply a server-committed change only when its clock is newer than
  the one they last knew for the record.

## Session flow

1. Client opens the socket and sends `HelloRequest` (device id, name, app
   version, last seen sequence number).
2. Server replies `HelloResponse`. When the client's sequence is behind the
   retained event log (or zero, on first pairing) the response demands a
   snapshot; otherwise the server streams the missed events.
3. Snapshot: the server sends every record including tombstones, so a device
   joining late sees server-side deletions instead of resurrecting records.
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
