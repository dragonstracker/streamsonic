# Streamsonic API reference

Base path: `/streamer/api/v3`

Media delivery lives outside the API prefix, at `/{name}/` with `/stream/{name}/` kept as an alias.
See [Delivery endpoints](#delivery-endpoints).

## Authentication

When `edit_auth` is configured, every route below `/streamer/api/v3` requires either:

```
Authorization: Basic base64(login:password)
Authorization: Bearer base64(login:password)
Authorization: Bearer <password>
?token=<password>
```

The `Bearer base64(login:password)` form is what the admin SPA sends, so it also
works from scripts that reuse a captured browser session.

`/ui_settings`, `/version`, `/monitoring/*` and all media routes are unauthenticated.
Playback can be gated separately with the stream `on_play` hook.

Failures return `401` with `WWW-Authenticate: Basic realm="streamsonic"`.

## Errors

Every error uses the same envelope:

```json
{
  "error": "stream not found",
  "status": 404
}
```

| Status | Meaning |
| --- | --- |
| 400 | Malformed body, invalid time or duration, configuration rejected |
| 401 | Missing or wrong credentials |
| 404 | Unknown stream or resource |
| 409 | Stream name already exists |
| 500 | Internal failure |
| 502 | `on_play` backend unreachable |

## Configuration

### GET /config

```json
{
  "config_version": [1794570000, 3],
  "config": {
    "http": { "port": 8080 },
    "edit_auth": "admin",
    "streams": [
      {
        "name": "channel-1",
        "inputs": [{ "tshttp": { "url": "http://origin/live.ts" } }],
        "segment_duration": 6,
        "segment_count": 6,
        "dvr": { "reference": "main" }
      }
    ],
    "dvrs": [{ "name": "main", "root": "/var/lib/streamsonic/dvr", "expiration": 604800 }]
  },
  "text": "http {\n  port 8080;\n}\n..."
}
```

`config_version` is `[process start, revision]`. The revision increases on every
successful write.

### GET /config/text

Returns the configuration source as `text/plain`. This is the exact text that would
be written to disk.

### PUT /config

Two accepted bodies.

Configuration text:

```
Content-Type: text/plain

http {
  port 8080;
}
```

Snapshot JSON, matching the `config` object from `GET /config`:

```
Content-Type: application/json

{ "http": { "port": 8080 }, "streams": [] }
```

The new configuration is parsed and validated before it replaces the running one.
A rejected configuration leaves the server untouched and returns `400` with the
parser error. Successful writes are persisted atomically to the configured path.

```json
{ "config_version": [1794570000, 4] }
```

## Streams

### GET /streams

Array of stream snapshots:

```json
[
  {
    "name": "channel-1",
    "status": "online",
    "started": "2026-11-13T11:41:46Z",
    "comment": "",
    "media_info": {
      "tracks": [
        { "id": 0, "pid": 256, "type": "video", "codec": "h264",
          "width": 240, "height": 160, "fps": 24, "profile": "Baseline", "level": "3.0" },
        { "id": 1, "pid": 257, "type": "audio", "codec": "aac",
          "sample_rate": 48000, "channels": 2 }
      ]
    },
    "stats": {
      "bytes_in": 263168, "bytes_out": 180592, "segments": 24, "clients": 2,
      "input_errors": 0, "input_switches": 0, "bitrate": 177000,
      "last_publish": 1794570031105, "hls_segments": 6, "dash_segments": 6
    },
    "dvr": { "from": "...", "to": "...", "size": 295724, "entries": 3 },
    "config": {
      "name": "channel-1",
      "inputs": [ { "url": "tshttp://origin/live.ts", "type": "tshttp" } ],
      "segment_duration": 6,
      "segment_count": 6
    }
  }
]
```

`status` is `online`, `offline` or `error`. `last_error` appears when set.
`config` is the REST projection of the stream definition, so the admin UI renders
the list without a second request per row. Inputs are flat (`{"url": ..., "type":
...}`) rather than the type-keyed form used by the configuration file.

### POST /streams

```json
{ "name": "channel-2", "inputs": [{ "url": "tshttp://origin/live.ts" }] }
```

`201 Created` → `{ "name": "channel-2" }`. `409` if the name exists.
The stream starts immediately and the configuration is persisted.

### GET /streams/{name}

Stream snapshot plus its `config` object.

### PUT /streams/{name}

Replaces the stream definition. The body follows the same shape as the `stream`
object in the configuration snapshot. The name in the body must match the URL.

### DELETE /streams/{name}

Stops the stream, removes it from the configuration and releases its DVR archive.
`{ "deleted": "channel-2" }`.

### PUT /streams/{name}/start

Restarts ingestion inside the server lifetime. `{ "status": "started" }`.

### PUT /streams/{name}/stop

Stops ingestion and publishing. `{ "status": "stopped" }`.

### GET /streams/{name}/stats

Identical to the snapshot returned by `GET /streams/{name}` without the `config`.

### GET /streams/{name}/sessions

```json
[
  {
    "id": "58bd2fe935e20eeefbe1c64a4b3a90bf",
    "stream": "channel-1",
    "ip": "203.0.113.7",
    "protocol": "hls",
    "user_agent": "Mozilla/5.0",
    "referer": "https://example.com/",
    "started": "2026-11-13T11:42:05Z",
    "bytes": 119580,
    "played": 42
  }
]
```

### GET /streams/{name}/dvr

```json
{
  "enabled": true,
  "stream": "channel-1",
  "from": "2026-11-13T11:41:46Z",
  "to": "2026-11-13T11:42:47Z",
  "size": 295724,
  "entries": 24,
  "expiration": 604800,
  "ranges": [{ "from": "2026-11-13T11:41:46.772Z", "to": "2026-11-13T11:42:47.772Z" }],
  "locks": []
}
```

When DVR is not configured for the stream: `{ "enabled": false }`.

### POST /streams/{name}/dvr/lock

```json
{ "from": "2026-11-13T11:00:00Z", "duration": 1800, "comment": "incident" }
```

`from` accepts RFC 3339, unix seconds or unix milliseconds. `duration` is in seconds.
Locked ranges are exempt from retention cleanup. → `{ "locked": true }`.

### DELETE /streams/{name}/dvr/lock?from=...

Releases the lock that starts at `from`. → `{ "locked": false }`.

## Sessions

### GET /sessions?stream=channel-1

All sessions, optionally filtered by stream. Same object shape as
`GET /streams/{name}/sessions`.

## Server

### GET /server/stats

```json
{
  "version": "1.0.0",
  "go_version": "go1.22.2",
  "uptime": 3600,
  "streams": 2,
  "clients": 14,
  "sessions": 14,
  "segments": 720,
  "bytes_in": 1096980,
  "bytes_out": 250800,
  "goroutines": 9,
  "heap_alloc": 6000320,
  "sys": 16210960,
  "num_cpu": 4,
  "config_rev": 3
}
```

### GET /ui_settings

```json
{
  "title": "Streamsonic",
  "version": "1.0.0",
  "brand": "streamsonic",
  "edit_auth": true,
  "default_page": "streams",
  "locale": "en",
  "timezone": "UTC",
  "dvr_player": true,
  "stream_player": true,
  "webrtc_supported": false
}
```

### GET /monitoring/liveness

`{ "status": "ok" }`. Never fails while the process is alive.

### GET /monitoring/readiness

```json
{ "status": "ok", "streams": 2, "streams_online": 1 }
```

### GET /version

```json
{ "version": "1.0.0", "build": "go1.22.2", "edition": "streamsonic" }
```

## Delivery endpoints

Prefix: `/{name}/` (alias: `/stream/{name}/`)

| Path | Content type | Notes |
| --- | --- | --- |
| `index.m3u8` | `application/vnd.apple.mpegurl` | Live sliding window |
| `master.m3u8`, `playlist.m3u8` | same | Aliases of the live playlist |
| `<YYYY>/<MM>/<DD>/<HH>/<MM>/<SS>-<seq>.ts` | `video/mp2t` | Live segment |
| `index.mpd`, `manifest.mpd` | `application/dash+xml` | Dynamic manifest |
| `init-video.mp4`, `init-audio.mp4` | `video/mp4` | DASH initialization |
| `video-<n>.m4s`, `audio-<n>.m4s` | `video/mp4` | DASH fragments, `n` starts at 1 |
| `preview.json` | `application/json` | MSE manifest |
| `preview-init.mp4` | `video/mp4` | MSE initialization |
| `preview-<n>.m4s` | `video/mp4` | MSE fragment with video and audio |
| `info.json` | `application/json` | Stream snapshot |
| `media_info.json` | `application/json` | Tracks only |
| `ranges.json` | `application/json` | Recorded ranges |
| `archive-<from>-<duration>.m3u8` | `application/vnd.apple.mpegurl` | VOD playlist |
| `archive-<from>-<duration>.ts` | `video/mp2t` | Byte range of the archive |
| `timeshift_abs-<time>.m3u8` | playlist | Start at an absolute time |
| `timeshift_rel-<seconds>.m3u8` | playlist | Start at `now - seconds` |
| `rewind-<seconds>.m3u8` | playlist | Alias of relative time shift |
| `dvr-<from>-<duration>.ts` | `video/mp2t` | One archived chunk |

Playlists and manifests are served with `Cache-Control: no-cache`. Segments and
fragments are cacheable. Range requests on `.ts` archives stream directly from the
blob files without re-muxing.

Sessions are tracked with the `ss-session` cookie, so a player fetching many
segments counts as one viewer.

## MSE manifest

`preview.json`:

```json
{
  "stream": "channel-1",
  "init": "preview-init.mp4",
  "codecs": "avc1.42c01e,mp4a.40.2",
  "target": 6,
  "live": true,
  "segments": [
    { "n": 1, "from": "2026-11-13T11:41:46Z", "duration": 5.958, "size": 101831 }
  ]
}
```

Append `preview-init.mp4` first, then fragments in `n` order into a single
`SourceBuffer` with `codecs` as the MIME type. Fragment timestamps are absolute, so
seek to `buffered.end - 1.5` to start at the live edge.

## on_play authorization

```
stream channel-1 {
  on_play http://auth.example.com/check;
}
```

Every playback request performs:

```
GET http://auth.example.com/check?stream=channel-1&ip=<client>&token=<token>
```

`200` allows playback, anything else returns `403`. An unreachable backend returns
`502`.
