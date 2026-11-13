# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.3.2] - 2026-11-13

### Fixed

- Stream text search (`q` / `query`) filters server-side the same way Flussonic
  does: case-insensitive substring over name, title, comment, and input URLs.
- `select=` on `/streams` returns only the requested fields so chip/count
  queries stay light.

## [1.3.1] - 2026-11-13

### Fixed

- Config editor loads and saves the same way as Flussonic: `GET /config` with
  `Accept: text/plain` returns the config file body, and `PUT /config` accepts
  `text/plain` even when a charset is attached.
- `OPTIONS` requests answer `204` instead of falling through to a `405`, which
  stopped the red "method not allowed" toast in the panel.

### Changed

- License and upgrade menu entries are gone. `/license` reports open source;
  no license key is required.

## [1.3.0] - 2026-11-13

### Changed

- The admin panel is now the real Flussonic `admin3` SPA (React + MUI + the original
  CSS, icon font, locales, and empty-state art), driven by Streamsonic's API.
- `GET /streams`, `GET /sessions`, and the empty collection routes return the
  Flussonic envelope (`server_id`, `estimated_count`, `timing`, `next`, named list).
- `GET /config` returns the Flussonic stats payload the panel header reads
  (`total_streams`, `total_clients`, `input_kbit`, `output_kbit`, `uptime`).
- `GET /ui_settings` ships the same base64 fonts, logos, favicons, colors, and
  `menu_items` shape the original panel expects.

## [1.2.7] - 2026-11-13

### Fixed

- The panel settings endpoint reports the embedded font files by their real names
  (`custom-300`, `custom-400`, `custom-500`), so a client that resolves faces from the
  settings response finds them instead of the retired `fira-sans` files.

### Changed

- The stylesheet keeps only the icon glyphs the panel draws, and the stream row markup
  is built once instead of being rebuilt on every poll.

## [1.2.6] - 2026-11-13

### Changed

- The admin panel is rebuilt on the Flussonic design system: the imported three-weight
  `CustomFont` face, the `icomoon_admin` icon font, the `#001F77` navigation rail at
  65 px, the `#002DAB` primary and active-item fill, the 58 px page head with 21 px
  title, 34 px tabs with `#F2F4F7` inactive fill, the 33 px `#98A2B2` sort control and
  the 108 px stream row with the same five columns as the original.
- Icons across the rail, tabs and rows come from the original icon font instead of
  inline SVG, so shapes match at every size.
- Signing in validates the credentials against `server/stats` instead of downloading
  the whole stream catalog.

### Fixed

- A `401` from the API no longer sends `WWW-Authenticate: Basic`, so Chrome and Edge
  stop opening a native sign-in dialog over the panel. The SPA login form handles the
  credentials, and `curl -u login:password` still authenticates.

## [1.2.5] - 2026-11-13

### Fixed

- Saving the configuration no longer reorders `header.*` options on an input. The
  options keep the order they were written in, so a save leaves the file byte-identical
  apart from normalised whitespace.
- An explicit `disabled false;` inside a stream survives a save instead of being
  dropped.
- A flat `http 8081;` declaration stays flat instead of being rewritten as an
  `http { port 8081; }` block.
- A non-GET request to a path that is not an API route now answers `405` instead of
  falling through to the admin page. A PUT to a misspelled endpoint returned `200` with
  HTML and looked like it saved.

## [1.2.4] - 2026-11-13

### Fixed

- The admin API, the SPA bundle and the stylesheet are served gzip-encoded when the
  client accepts it. A 999-stream catalog drops from ~1 MB to ~61 KB per refresh, so
  the stream list reaches the browser in about a second instead of stalling.
- HLS manifests, DASH segments, DVR ranges and `preview.json` stay uncompressed so
  playback, byte ranges and MSE previews are never affected.

### Added

- One-click installer (`install.sh`) with platform detection, SHA-256 verification,
  idempotent config creation and a hardened systemd unit.
- Release workflow building `linux/amd64`, `linux/arm64`, `linux/armv6`,
  `darwin/amd64` and `darwin/arm64` archives plus `checksums.txt`, attached to the
  GitHub release for the pushed tag.
- CI workflow running `gofmt`, `go vet`, `go test`, a full build and `shellcheck`.

## [1.2.3] - 2026-11-13

### Fixed

- The binary reports the version of the release it was built from.

## [1.2.2] - 2026-11-13

### Fixed

- `install.sh` prints the HTTP port read from the deployed config instead of a
  hard-coded guess, and opens that port in `ufw` or `firewalld` when either is active.

## [1.2.1] - 2026-11-13

### Fixed

- A config without a `dvr` block falls back to `/var/lib/streamsonic/dvr` on an
  installed host instead of writing archives into `/etc/streamsonic/dvr`.

## [1.2.0] - 2026-11-13

### Added

- Native `ondemand <name> { … }` blocks. They parse as streams, render back with the
  `ondemand` keyword and start only when a player requests their manifest.
- `disabled` on streams: disabled entries are kept in the config, listed in the UI and
  never started.
- `push <name> { url …; }` blocks are parsed and rendered verbatim.
- Directives that this build does not act on (`transcoder`, `drm`, `epg`, `meta`,
  `thumbnails`, `domain`, `logo`, `substyle`) are preserved in a lossless passthrough
  list, re-emitted in the same order and exposed to the UI, so a full Flussonic
  config round-trips without losing a line.
- Stream list shows the `title` (with the stream name beneath it), the real
  always-started/on-demand mode, the disabled state and whether a transcoder is
  configured, and honours every Sort By column.

### Changed

- `input` schemes follow Flussonic semantics: `tshttp://` and `hls://` fetch over
  HTTP, `tshttps://` and `hlss://` over HTTPS, and the wrapper is restored when the
  config is written back.
- `#` is only a comment when it starts a token, so `size=960x-1:fit:#000000` parses.

### Fixed

- Loading a production config no longer aborts on the first unknown stream directive.
- Idle on-demand streams cost almost nothing: 999 configured streams boot in ~12 MB
  of RSS with 6 goroutines.

## [1.1.3] - 2026-11-13

### Added

- Media resources are also served at the Flussonic root paths `/<stream>/<file>`
  (`index.m3u8`, `index.mpd`, `preview.json`, `preview-init.mp4`, `preview-<n>.m4s`,
  `video-<n>.m4s`, `audio-<n>.m4s`, `dvr-*.ts`, `timeshift_*`, `rewind-*`,
  `info.json`, `media_info.json`, `ranges.json`, plus nested `*.ts` segments).
  `/stream/<stream>/<file>` stays available as an alias, and the admin player now
  uses the root form.

### Fixed

- Stream input JSON accepts an explicit `type`, and non-inferable types survive the
  config rewrite. An HLS playlist URL stored as `input hls https://…m3u8;` no longer
  collapses to `tshttp` and fails with `EOF`.
- `hls://`, `tshttp://`, `rtsp://` style wrapper prefixes are stripped when the inner
  URL carries its own scheme, matching Flussonic config syntax.
- The systemd unit grants write access to `/etc/streamsonic`, so API and UI config
  edits persist instead of failing with `read-only file system`.

## [1.1.2] - 2026-11-13

### Fixed

- `install.sh` no longer exits with `tmp: unbound variable` when `set -u` is in
  effect: the download workspace is a script-scope directory removed by a single
  `EXIT` trap.
- Default DVR root now resolves from the first configured `dvr` block (`default`,
  then `main`, then alphabetically), so a packaged install writes recordings under
  `/var/lib/streamsonic/dvr` instead of the read-only `/etc/streamsonic/dvr` that
  `ProtectSystem=full` forbids.

## [1.1.1] - 2026-11-13

Release pipeline fix. The 1.1.0 tag produced no release assets because the
`dvr/` ignore rule also matched `internal/dvr/`, so `storage.go` never entered
the repository and every matrix build failed.

### Fixed

- `.gitignore` now anchors the runtime DVR directory (`/dvr/`) so source packages
  under `internal/` are always tracked.
- Release archives use the exact asset names the installer resolves:
  `streamsonic-linux-amd64`, `streamsonic-linux-arm64`, `streamsonic-linux-armv6`,
  `streamsonic-darwin-amd64`, `streamsonic-darwin-arm64`.
- All Go sources carry LF line endings and no byte-order mark, so `gofmt -l`
  reports a clean tree in CI.

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
