# Streamsonic

A compact, production-oriented live media server written in Go. It ingests MPEG-TS
over HTTP, UDP or file, demuxes the transport stream in pure Go, and publishes the
same media simultaneously as HLS, DASH, fragmented MP4 for MSE preview and a
time-shifted DVR archive. The HTTP surface, configuration dialect and admin UI
follow the conventions of Flussonic Media Server, so existing supervision,
players and automation keep working.

Single binary. No CGO, no external ffmpeg, no runtime dependencies.

```
ingest ──► MPEG-TS demux ──► segmenter ──┬──► HLS (mpegts)
                                        ├──► DASH (fMP4)
                                        ├──► preview (muxed fMP4 / MSE)
                                        └──► DVR (hourly blobs + index)
```

## Install

```bash
curl -fsSL https://github.com/dragonstracker/streamsonic/releases/latest/download/install.sh \
  | sudo DRMSONIC_TAG=latest bash
```

The installer detects `linux/amd64`, `linux/arm64` or `linux/armv6`, verifies the
release checksum, installs `/usr/local/bin/streamsonic`, writes
`/etc/streamsonic/streamsonic.conf`, registers a hardened systemd unit and prints
the admin URL.

| Variable | Default | Meaning |
| --- | --- | --- |
| `DRMSONIC_TAG` | `latest` | release tag to install |
| `DRMSONIC_PREFIX` | `/usr/local` | install prefix |
| `DRMSONIC_PORT` | `8080` | port written to a fresh config |
| `DRMSONIC_NO_START` | `0` | set to `1` to install without starting |


## Contents

- [Features](#features)
- [Quick start](#quick-start)
- [Build](#build)
- [Configuration](#configuration)
- [Delivery URLs](#delivery-urls)
- [REST API](#rest-api)
- [Admin UI](#admin-ui)
- [Deployment](#deployment)
- [Design notes](#design-notes)
- [Layout](#layout)

## Features

| Area | Detail |
| --- | --- |
| Ingest | `tshttp`, `udp` (unicast + multicast), `file`, `hls` polling, multi-input failover by priority |
| Demux | PAT/PMT/PES, 33-bit 90 kHz timestamps, H.264 and HEVC SPS parsing, AAC ADTS parsing |
| HLS | Sliding-window playlist, timestamped segment paths, `EXT-X-DISCONTINUITY` on gaps |
| DASH | Dynamic MPD, `SegmentTemplate` + `SegmentTimeline`, separate video and audio adaptations |
| Preview | Multi-track fragmented MP4 fragments playable by a plain `<video>` element through MSE |
| DVR | Hourly append-only blobs, atomic JSON index, range reads, retention and storage limits, lock/unlock |
| Time shift | `archive-`, `timeshift_abs-`, `timeshift_rel-`, `rewind-` playlists |
| Config | NGINX-style parser, AST round-trip, validation, atomic write and rename |
| API | REST v3 compatible routes for config, streams, DVR, sessions, monitoring |
| Auth | HTTP Basic and Bearer token, optional `on_play` authorization hook |
| Ops | Event sinks over HTTP, session accounting, liveness and readiness probes |

## Quick start

```bash
go build -o streamsonic ./cmd/streamsonic
./streamsonic -config streamsonic.conf
```

If the configuration file is missing, a commented starter file is created next to it.
The default listener is `:8080`.

Minimal configuration:

```
http {
  port 8080;
}

stream channel-1 {
  input tshttp://origin.example.com/live.ts;
  segment_duration 6;
  segment_count 6;
}
```

Play it:

```
http://localhost:8080/stream/channel-1/index.m3u8
http://localhost:8080/stream/channel-1/index.mpd
http://localhost:8080/
```

## Build

Requirements: Go 1.22 or newer. Nothing else.

```bash
make build          # ./bin/streamsonic
make build-linux    # static linux/amd64 binary
make vet            # go vet
make fmt            # gofmt
```

The binary is self-contained: web assets are embedded, media packaging is pure Go.

## Configuration

The configuration language is the block/argument form used by Flussonic, parsed into
an AST and then into typed structs. Every accepted directive is round-tripped back to
text, so the API, the UI editor and the file on disk stay identical.

### http

```
http {
  port 8080;
  tls true;
  cert /etc/streamsonic/tls/fullchain.pem;
  key /etc/streamsonic/tls/privkey.pem;
}
```

### edit_auth

```
edit_auth admin s3cret;
```

When set, every `/streamer/api/v3/*` route requires Basic credentials
(`admin:s3cret`) or a Bearer token equal to the password. Playback routes stay open
unless `on_play` is configured.

### dvr

```
dvr main {
  root /var/lib/streamsonic/dvr;
  expiration 604800;
  storage_limit 50000000000;
}
```

`expiration` is in seconds. `storage_limit` is in bytes. The root directory is
created on demand; each stream gets its own sub-directory.

### stream

```
stream channel-1 {
  input tshttp://origin.example.com/live.ts priority=10 source_timeout=10;
  input udp://239.0.0.1:1234 priority=5;
  backup tshttp://backup.example.com/live.ts;
  segment_duration 6;
  segment_count 6;
  clients_timeout 30;
  dvr main;
}
```

Common stream directives:

| Directive | Meaning |
| --- | --- |
| `input URL [key=value ...]` | Ingest source. Repeat for alternates; higher `priority` wins |
| `backup URL` | Secondary source used when inputs fail |
| `segment_duration N` | Target segment length in seconds (GOP aligned) |
| `segment_count N` | Segments kept in the live window |
| `clients_timeout N` | Seconds before an idle session is reaped |
| `dvr NAME` | Reference a `dvr` block |
| `on_play URL` | Authorization hook called before playback |
| `title`, `comment`, `static`, `ondemand` | Metadata and behaviour flags |
| `protocols { hls true; dash true; ... }` | Per-protocol enablement |

Input options: `priority`, `source_timeout`, `video_timeout`, `audio_timeout`,
`retry_limit`, `max_retry_timeout`, `user_agent`, `headers` and arbitrary `key=value`
pairs which are forwarded as HTTP headers.

### Segment sizing

Segments are cut on video keyframes. A `segment_duration` shorter than the encoder
GOP cannot produce shorter segments; the server never breaks an access unit.

## Delivery URLs

All media lives under `/stream/<name>/`.

| Path | Description |
| --- | --- |
| `index.m3u8` | Live HLS media playlist |
| `master.m3u8` | Single-variant master playlist |
| `index.mpd` | Dynamic DASH manifest |
| `preview.json` | MSE preview manifest (codecs, init path, fragment list) |
| `preview-init.mp4` | MSE initialization segment |
| `preview-<n>.m4s` | MSE fragment (video + audio in one `moof`) |
| `init-video.mp4`, `init-audio.mp4` | DASH initialization segments |
| `video-<n>.m4s`, `audio-<n>.m4s` | DASH media fragments |
| `info.json` | Stream snapshot: status, stats, media info, DVR summary |
| `media_info.json` | Track description only |
| `ranges.json` | Recorded DVR ranges |
| `archive-<from>-<duration>.m3u8` | VOD playlist over the archive |
| `archive-<from>-<duration>.ts` | Archived transport stream for the range |
| `timeshift_abs-<time>.m3u8` | Playlist from an absolute start time |
| `timeshift_rel-<seconds>.m3u8` | Playlist from `now - seconds` |
| `rewind-<seconds>.m3u8` | Alias of relative time shift |
| `dvr-<from>-<duration>.ts` | One archived chunk |

`<from>` accepts RFC 3339 or a unix timestamp in seconds or milliseconds.

## REST API

Base path: `/streamer/api/v3`. Full reference in [docs/API.md](docs/API.md).

```
GET    /config                   configuration snapshot
GET    /config/text              configuration source
PUT    /config                   replace configuration (JSON or text)
GET    /streams                  list streams
POST   /streams                  create a stream
GET    /streams/{name}           stream details with config
PUT    /streams/{name}           update a stream
DELETE /streams/{name}           remove a stream
PUT    /streams/{name}/start     start ingestion
PUT    /streams/{name}/stop      stop ingestion
GET    /streams/{name}/stats     live counters and media info
GET    /streams/{name}/sessions  viewers
GET    /streams/{name}/dvr       archive status, ranges and locks
POST   /streams/{name}/dvr/lock  pin a time range
DELETE /streams/{name}/dvr/lock  release a lock
GET    /sessions                 all sessions
GET    /server/stats             process and delivery totals
GET    /ui_settings              UI metadata
GET    /monitoring/liveness      liveness probe
GET    /monitoring/readiness     readiness probe
GET    /version                  build information
```

## Admin UI

The admin SPA is served at `/admin/` and mirrors the Flussonic layout and route
map, so muscle memory and bookmarks carry over:

| Route | Page |
| --- | --- |
| `#/streams` | Media, with tabs Streams / Templates / Multiplexers / Sources / VODs / DVB cards |
| `#/streams/<name>` | Stream detail: Overview / Input / Transcoder / DVR / Output / EPG / Auth / Play sessions |
| `#/create?entity=stream` | New stream form |
| `#/pulse` | Node health and delivery totals |
| `#/config` | Settings / Config editor / DVR / Auth / Auth backends / Events |
| `#/cluster`, `#/iptv/users`, `#/watcher`, `#/support` | Cluster, IPTV, IP Cameras, Support |

The design system is a direct match: primary `#002DAB`, navigation rail `#001F77`,
accent `#FF0D70`, body text `#232323`, panel border `#D2D9DF`, tab strip `#F2F4F7`,
base unit 4px, Fira Sans at 14px with 12px table body text. The rail is a 65px
fixed column and content is inset `84px` left / `19px` right, exactly as upstream.
See [docs/UI.md](docs/UI.md) for the token table and the theme contract.

`GET /streamer/api/v3/ui_settings` returns the white-label theme document
(`title`, `brand`, `colors`, `fonts`, `menu_items`, `locales`) so a deployment can
be re-branded without rebuilding the SPA.

The preview player uses `MediaSource` with a single `SourceBuffer`; the server muxes
video and audio into one fragment so no player library is required.

## Deployment

```bash
sudo install -m 0755 bin/streamsonic /usr/local/bin/streamsonic
sudo install -d /etc/streamsonic /var/lib/streamsonic/dvr
sudo install -m 0644 streamsonic.conf.example /etc/streamsonic/streamsonic.conf
sudo install -m 0644 deploy/streamsonic.service /etc/systemd/system/streamsonic.service
sudo systemctl enable --now streamsonic
```

`deploy/streamsonic.service` runs as an unprivileged user with a hardened unit file.
Logs go to the journal: `journalctl -u streamsonic -f`.

Reverse proxy example (HLS needs no buffering):

```
location /stream/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_buffering off;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
}
```

## Design notes

**Why Go.** The workload is thousands of tiny allocations, timers and network
reads. Goroutines plus the standard library keep the data plane simple and the
scheduler predictable. Pure-Go TS and fMP4 muxers avoid CGO and ffmpeg, which keeps
the binary portable and the deploy trivial. Measured footprint for one 240x160
stream with DVR enabled: ~18 MB resident, 9 OS threads.

**Segmentation is keyframe driven.** A segment closes when a video keyframe arrives
after the target duration. `EXT-X-DISCONTINUITY` is emitted whenever the wall-clock
start of a segment does not continue the previous one, which covers upstream
restarts, `file` loops and PTS resets.

**Audio and video stay on one clock.** Video access units take their timestamps from
PES headers; audio is re-anchored at every PES that carries a PTS and interpolated
between anchors at 1024 samples per frame. The MSE muxer writes absolute `tfdt`
values so both tracks remain in sync across fragments.

**DVR is append-only.** Each stream writes hourly blobs and an index of
`(start, duration, offset, size)` entries. The index is written atomically. Reads
resolve through byte offsets, so a range request never re-muxes media.

**Configuration is code.** The lexer/parser produce a node tree; typed structs are
mapped both ways; validation rejects impossible ports, duplicate names and dangling
DVR references before anything is applied.

## Layout

```
cmd/streamsonic        entrypoint, flags, signals
internal/config        lexer, AST, typed model, validation, rendering
internal/media         TS demux, NAL/SPS/ADTS parsing, TS mux, fMP4 mux, segmenter
internal/packager      HLS playlist, DASH manifest, live ring buffer
internal/dvr           archive storage, index, ranges, locks
internal/stream        ingest drivers, stream lifecycle, failover, metrics
internal/api           REST v3 routes, auth, config store, media routing
internal/webui         embedded admin UI
internal/events        event bus and HTTP sinks
internal/session       viewer registry and idle reaper
```

## License

MIT.
