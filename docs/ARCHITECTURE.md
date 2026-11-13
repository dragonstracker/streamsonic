# Architecture

Streamsonic is a single process with one writer per stream and many readers.
Everything in the media path is pure Go: no CGO, no ffmpeg, no shared memory.

```
                    ┌─────────────────────────────────────────────┐
   tshttp / udp     │            stream.Stream (goroutine)        │
   file / hls  ────►│  ingest → demux → segmenter → publish       │
                    └───────┬─────────┬──────────┬──────────┬─────┘
                            │         │          │          │
                        HLS ring  DASH ring  preview ring  dvr.Archive
                            │         │          │          │
                     api.mediaHandler (HTTP readers, no locks on the data plane)
```

## Packages

| Package | Responsibility |
| --- | --- |
| `internal/config` | Lexer, AST, typed model, validation, text and JSON rendering, atomic store |
| `internal/media` | MPEG-TS demuxer, NAL/SPS/ADTS parsers, TS muxer, fMP4 muxer, segmenter |
| `internal/packager` | Live ring buffer, HLS playlists, DASH manifest |
| `internal/dvr` | Hourly blob storage, index, range reader, retention, locks |
| `internal/stream` | Input drivers, stream lifecycle, failover, metrics, manager |
| `internal/api` | REST v3 handlers, authentication, media routing, config store |
| `internal/session` | Viewer registry and idle reaper |
| `internal/events` | Event bus with filtered HTTP sinks |
| `internal/webui` | Embedded admin UI assets |

## Ingest and demux

`OpenSource` maps an input type to an `io.ReadCloser`. The demuxer consumes raw
bytes through a buffered reader and resynchronises on the 0x47 sync byte.

For each PID the demuxer keeps `trackState`: a PES assembly buffer, the current PES
timestamp, the number of payload bytes still expected, and the access-unit
timestamp. PSI sections are parsed once they fit the payload of a PUSI packet.

Video access units are cut at the start of the next picture. `IsPictureStart`
inspects `first_mb_in_slice` through the exp-Golomb reader, which is the standard
access-unit boundary heuristic for H.264 and is replaced by the first-slice flag for
HEVC. Audio frames are parsed from ADTS headers; a partial frame is never treated as
corruption, the demuxer simply waits for the rest of the PES.

Timestamps:

- video takes PTS and DTS from the PES header of the packet in which the access unit
  starts, with the previous frame step used for additional access units inside one
  PES;
- audio is re-anchored at every PES that carries a PTS and advances 1024 samples per
  frame between anchors.

## Segmentation

`media.Segmenter` accumulates frames and closes a segment when a keyframe arrives
after the target duration. It also closes on a backwards PTS jump of more than 30
seconds, which covers upstream restarts, and on a hard frame cap as a safety valve.

Timing comes from video PTS when the segment contains video, so audio drift cannot
stretch a segment. The segment start is converted to wall clock through a base time
captured at the first frame, which gives every segment a timestamp used in its file
name and in the DVR index.

## Publishing

`Stream.publish` receives a closed segment and fans it out:

1. **HLS** — the segment is re-muxed to MPEG-TS (PAT, PMT, PES with PCR) and stored
   in a ring buffer of `segment_count` entries.
2. **DASH** — video and audio frames are written as separate fragmented MP4
   fragments with absolute `tfdt` values.
3. **Preview** — one `moof` carries a `traf` per track, so the browser appends a
   single fragment into a single `SourceBuffer`.
4. **DVR** — the same TS bytes are appended to the archive with their wall-clock
   start and duration.

The ring buffer stores immutable byte slices. HTTP readers take a snapshot of the
ring and never block the writer.

## MPEG-TS writer

`TSMuxer` assigns PIDs, writes PAT and PMT with a pointer field and a CRC32, then
writes each access unit as a PES packet split across 188-byte packets. PCR is carried
in the adaptation field of the first packet of each video access unit. Continuity
counters are per PID.

## Fragmented MP4 writer

`TrackMuxer` builds the initialization segment once per track and fragments
afterwards. Each fragment is `moof` + `mdat`:

- `tfhd` with default-base-is-moof,
- `tfdt` with the absolute decode time of the first sample,
- `trun` with per-sample duration, size, flags and composition offset,
- `mdat` containing length-prefixed NAL units (AVCC) or raw AAC payloads.

`MultiMuxer` builds a `moof` with several `traf` boxes and a single `mdat`, patches
the data offsets of each `trun` after the layout is known, and returns one buffer.

## DVR

`dvr.Archive` appends to a blob per hour (`YYYYMMDDTHH.ts`) and records
`(start, duration, offset, size)` in `index.json`. The index is written by
replacing a temporary file, so a crash leaves either the old or the new index.

Reads select entries by time, merge contiguous ones and serve the corresponding byte
ranges. Retention drops entries outside `expiration` and beyond `storage_limit`,
skipping locked ranges, then truncates or removes blobs that are no longer
referenced.

## Configuration store

`api.Store` holds the current `*config.Config` behind a mutex, hands out deep copies,
and persists every replacement atomically. Writers call `Update`, which mutates a
clone, validates it, writes it to disk and only then swaps it in, so a failing write
never leaves memory and disk out of sync.

## Concurrency model

- One goroutine per stream owns the ingest, demux and publish path. No locks are
  needed inside a stream between frames.
- `Stream` mutexes guard configuration, media info and muxer pointers, which are
  read by HTTP handlers.
- Rings are lock protected and copy snapshots for readers.
- Counters are atomics.
- The HTTP server is stateless apart from the shared manager, store and session
  registry.

## Failure handling

- Input failures escalate through an exponential backoff and rotate to the next
  input by priority, then to the backup source.
- A missing `on_play` backend returns 502 instead of allowing playback.
- A configuration that fails validation is rejected and the previous one keeps
  running.
- DVR write failures are reported through the event bus and never stop publishing.
