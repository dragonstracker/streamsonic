# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- One-click installer (`install.sh`) with platform detection, SHA-256 verification,
  idempotent config creation and a hardened systemd unit.
- Release workflow building `linux/amd64`, `linux/arm64`, `linux/armv6`,
  `darwin/amd64` and `darwin/arm64` archives plus `checksums.txt`, attached to the
  GitHub release for the pushed tag.
- CI workflow running `gofmt`, `go vet`, `go test`, a full build and `shellcheck`.

## [1.1.0] - 2026-11-13

Admin UI rebuilt as a 1:1 functional match of the reference interface, using design
tokens measured from a live Flussonic 26.11 node.

### Added

**Admin UI**

- Flussonic route map: `#/streams`, `#/streams/<name>`, `#/create?entity=stream`,
  `#/pulse`, `#/config` (+ `editor`, `dvrs`, `auth`, `backends`, `events`),
  `#/cluster`, `#/iptv/users`, `#/watcher`, `#/support`.
- 65px navigation rail on `#001F77` with inline SVG icons, active marker and a
  bottom Logout / language block.
- Media page with the `+` create button, tab strip (Streams, Templates,
  Multiplexers, Sources, VODs, DVB cards) and the `Sort By:` column row.
- Stream rows with index, name, start mode, status, input URL, uptime, bitrate,
  transcoder/archive/push summary, `More` link and an inline enable switch.
- Stream detail with Overview / Input / Transcoder / DVR / Output / EPG / Auth /
  Play sessions, live MSE preview, input and output media info, delivery URLs and
  embed snippet.
- Pulse page with node health and delivery totals.
- Config pages: summary, raw editor with validation on save, DVR storage, editor
  authorization, auth backends, event sinks.
- Login screen with the branded wordmark and the `MediaServerAuth` credential
  convention.
- `ui_settings` now returns the reference white-label contract: `title`, `product`,
  `brand`, `colors`, `fonts`, `locales`, `menu_items`.

**Packaging**

- Fira Sans (SIL OFL 1.1) bundled as latin-subset WOFF2 at weights 300/400/500,
  matching the reference `CustomFont` metrics.
- Design tokens documented in `docs/UI.md`, including the exact palette, type
  scale and spacing used by the reference deployment.

### Changed

- `GET /streams` and `GET /streams/{name}/stats` now include a `config` object and
  render inputs in the REST form (`{"url": ...}`) instead of the type-keyed config
  form.
- `GET /config` returns the REST projection of the snapshot.
- `PUT /streams/{name}` from the UI merges the full stream config, so editing the
  title no longer drops the input.
- `POST`/`PUT` accept `Authorization: Bearer base64(login:password)` in addition to
  Basic auth and a bare bearer token.
- The admin SPA is served at `/admin/` and `/admin/assets/` in addition to `/`.

## [1.0.0] - 2026-11-13

Initial release. A complete pure-Go live media server with the delivery surface,
configuration dialect and API shape of Flussonic Media Server.

### Added

**Ingest**

- MPEG-TS demuxer: PAT/PMT discovery, PES reassembly, continuity counter handling,
  stream resynchronisation, 33-bit 90 kHz timestamp decoding.
- Inputs: `tshttp` (HTTP progressive TS), `udp` (unicast and multicast), `file`
  with wall-clock pacing and looping, and `hls` playlist polling.
- Multi-input failover by priority with an additional `backup` source and
  exponential reconnect backoff.
- Codec probing: H.264 and HEVC SPS parsing (resolution, profile, level, chroma,
  bit depth), AAC ADTS parsing (sample rate, channels, profile).

**Packaging**

- HLS: sliding-window media playlist, master playlist, timestamped segment names,
  `EXT-X-TARGETDURATION`, `EXT-X-MEDIA-SEQUENCE`, `EXT-X-DISCONTINUITY-SEQUENCE`,
  `EXT-X-PROGRAM-DATE-TIME`, gap detection.
- DASH: dynamic MPD with `SegmentTemplate` + `SegmentTimeline`, separate video and
  audio adaptation sets, configurable availability window.
- MSE preview: multi-track fragmented MP4 (`moof` with several `traf` boxes) so a
  plain `<video>` element can play video and audio from a single `SourceBuffer`.
- Pure-Go ISO BMFF writer: `ftyp`, `moov` (`mvhd`, `trak`, `tkhd`, `mdia`, `mdhd`,
  `hdlr`, `minf`, `dinf`, `stbl`, `mvex`, `trex`), `moof` (`mfhd`, `traf`, `tfhd`,
  `tfdt`, `trun`) and `mdat` with runtime data-offset patching.
- Pure-Go MPEG-TS writer: PAT/PMT with CRC32, PCR clock, PES with PTS/DTS,
  adaptation field stuffing.

**Storage**

- DVR with hourly append-only blobs and an atomic JSON index of
  `(start, duration, offset, size)` entries.
- Retention by `expiration` and `storage_limit`, range selection, byte-offset range
  reader, lock and unlock for protected windows.
- Archive, absolute, relative and rewind playlists plus direct chunk access.

**Configuration**

- Lexer and recursive-descent parser for the NGINX-style directive/block dialect.
- Typed model for HTTP, TLS, authentication, streams, inputs, DVR, auth backends and
  event sinks with bidirectional rendering.
- Validation of ports, identifiers, input URLs and DVR references.
- Atomic configuration persistence (write to temporary file, rename).
- `PUT /config` accepts either configuration text or a JSON snapshot.
- Starter configuration generated when the file does not exist.

**API and UI**

- REST v3 routes for configuration, streams, start/stop, stats, sessions, DVR,
  server statistics, UI settings, liveness and readiness.
- HTTP Basic and Bearer authentication with constant-time comparison.
- Session registry keyed by cookie with idle reaping and byte accounting.
- Embedded admin UI: dashboard, per-stream page with live MSE preview and media
  info, DVR ranges, server health, configuration editor.

**Operations**

- Single static binary, no CGO, no external processes.
- Event bus with HTTP sinks, level filtering and retry.
- Graceful shutdown on `SIGINT`/`SIGTERM`.
- systemd unit with sandboxing and an example configuration.

### Fixed during development

- PAT/PMT sections were emitted without a pointer field, which made the tables
  unreadable for demuxers. Tables are now spec-compliant.
- Audio access units were truncated at PES boundaries, dropping roughly three
  quarters of AAC frames. Incomplete-frame detection now waits for more data instead
  of resynchronising into the middle of a frame.
- Audio timestamps drifted after upstream restarts because the accumulator was never
  re-anchored. Audio is now re-anchored at every PES carrying a PTS.
- Segment durations were computed across mixed audio and video timelines, producing
  40-second segments. Timing is now driven by video PTS.
- `file` inputs were consumed as fast as the disk allowed. Inputs are now paced to
  the media clock, which also keeps the live window at real time.
- Starting a stream through the API bound the stream lifetime to the HTTP request.
  Restarts now run inside the server lifetime context.
