package packager

import (
	"fmt"
	"strings"
	"time"
)

// SegmentName renders the timestamp based path used by every playlist.
func SegmentName(from time.Time, seq int) string {
	return fmt.Sprintf("%04d/%02d/%02d/%02d/%02d/%02d-%05d",
		from.Year(), int(from.Month()), from.Day(), from.Hour(), from.Minute(), from.Second(), seq%100000)
}

func MediaPlaylist(r *Ring, segName func(*Chunk) string) string {
	chunks := r.Snapshot()
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	target := r.Target()
	secs := int(target.Seconds())
	if secs < 1 {
		secs = 1
	}
	if target > time.Duration(secs)*time.Second {
		secs++
	}
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", secs)
	if len(chunks) == 0 {
		b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
		return b.String()
	}
	fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:%d\n", chunks[0].Seq)
	fmt.Fprintf(&b, "#EXT-X-DISCONTINUITY-SEQUENCE:%d\n", r.DiscSeq())
	fmt.Fprintf(&b, "#EXT-X-PROGRAM-DATE-TIME:%s\n", chunks[0].From.UTC().Format("2006-01-02T15:04:05.000Z"))
	for _, c := range chunks {
		if c.Disc {
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		fmt.Fprintf(&b, "#EXTINF:%.3f,\n%s\n", c.Duration.Seconds(), segName(c))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

// LivePlaylist is MediaPlaylist without the end marker.
func LivePlaylist(r *Ring, segName func(*Chunk) string) string {
	return strings.ReplaceAll(MediaPlaylist(r, segName), "#EXT-X-ENDLIST\n", "")
}

func MasterPlaylist(stream, query string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=3000000\n%s/index.m3u8\n", stream)
	if query != "" {
		b.WriteString("# streamsonic: query token forwarded by player\n")
	}
	return b.String()
}
