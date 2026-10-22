# kioku-sync

The self-hosted sync server for [Kioku](https://github.com/nawocci/kioku), a
Mihon fork. Each server hosts any number of libraries; one API key identifies
one library, and every device that pairs with the key keeps that library in
sync in real time: manga, chapters, reading history, categories, tracking,
settings, and source names. It also serves a small read-only web dashboard
for watching the library's devices and recent changes.

The wire protocol lives in [PROTOCOL.md](PROTOCOL.md).

## Running

With Docker:

```sh
docker compose up -d
```

Or build and run the binary directly:

```sh
go build
./kioku-sync serve
```

The server listens on `:8756` (override with `-addr` or `KIOKU_SYNC_ADDR`)
and keeps everything in a single SQLite database under the data directory
(`-data` or `KIOKU_SYNC_DATA`, `/data` in the container).

## Pairing devices

Keys are minted from the server console only; the server has no key
management interface.

```sh
docker exec -it kioku-sync kioku-sync key -label "my library"
```

The command prints a `kioku_…` key once — the server stores only its hash,
so it cannot be shown again. Paste the key, together with the server
address, into Kioku's Settings → Sync screen on every device that should
join the library.

A key grants full read and write access to its library. To revoke one,
delete the library's row; all of its devices, records, and events follow
via cascade:

```sh
sqlite3 data/kioku-sync.db "DELETE FROM libraries WHERE label = 'my library'"
```

## Transport security

The server itself speaks plain HTTP; whether that is safe depends entirely
on the network path between devices and the server:

- **Private network / Tailscale**: traffic stays inside the overlay, so the
  plain HTTP port is fine. Nothing confidential leaves the tunnel.
- **Cloudflare Tunnel or a reverse proxy**: terminate TLS at the proxy and
  forward to the container's `:8756`. WebSockets work through both; no
  extra configuration is needed for the `/ws` endpoint beyond enabling
  WebSocket forwarding in the proxy.
- **Directly exposed to the internet**: do not skip TLS. Over plain HTTP the
  API key — and with private settings enabled, source and tracker logins —
  is readable by anything on the path. Put the server behind a TLS reverse
  proxy (Caddy, nginx, Traefik) with a certificate before opening the port.

Kioku warns when pairing with a plain `http://` address that is not private
or localhost, so an accidental cleartext setup is hard to miss.

The dashboard transmits the same bearer key, so the same rules apply to
opening it in a browser.

## Upgrading

The sync protocol is versioned and checked strictly in both directions
during the WebSocket handshake: a client and server on different protocol
versions refuse to talk rather than misinterpreting each other. Upgrade the
server and all devices together whenever a release notes a protocol change.
The `/healthz` endpoint reports the server's protocol version.

## Development

```sh
go test ./...
npm --prefix web ci && npm --prefix web run build   # dashboard
go build                                            # embeds web/build
```
