package packager

import (
	"fmt"
	"strings"
	"time"
)

type TrackInfo struct {
	ID        uint32
	MimeType  string
	Codec     string
	Timescale uint32
	Width     int
	Height    int
	FPS       float64
	Bandwidth int
	Channels  int
}

type DashParams struct {
	Video         TrackInfo
	Audio         TrackInfo
	Window        time.Duration
	AvailableFrom time.Time
}

func MPD(p DashParams, video, audio *Ring) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	fmt.Fprintf(&b, `<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="dynamic" profiles="urn:mpeg:dash:profile:isoff-live:2011"`)
	fmt.Fprintf(&b, ` availabilityStartTime="%s" publishTime="%s"`, p.AvailableFrom.UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, ` minimumUpdatePeriod="PT2.000S" minBufferTime="PT2.000S" timeShiftBufferDepth="PT%.3fS" suggestedPresentationDelay="PT%.3fS">`+"\n",
		p.Window.Seconds(), maxFloat(p.Video.FPS*2, 6))
	b.WriteString(`  <Period id="0" start="PT0.000S">` + "\n")
	if video != nil && video.Len() > 0 {
		writeAdaptation(&b, "0", p.Video, video, "video", p.Window)
	}
	if audio != nil && audio.Len() > 0 {
		writeAdaptation(&b, "1", p.Audio, audio, "audio", p.Window)
	}
	b.WriteString("  </Period>\n</MPD>\n")
	return b.String()
}

func writeAdaptation(b *strings.Builder, id string, t TrackInfo, r *Ring, kind string, window time.Duration) {
	chunks := r.Snapshot()
	if len(chunks) == 0 {
		return
	}
	fmt.Fprintf(b, `    <AdaptationSet id="%s" mimeType="%s" segmentAlignment="true" startWithSAP="1"`, id, t.MimeType)
	if kind == "video" {
		fmt.Fprintf(b, ` maxWidth="%d" maxHeight="%d" maxFrameRate="%g" par="16:9"`, t.Width, t.Height, t.FPS)
	}
	b.WriteString(">\n")
	fmt.Fprintf(b, `      <Representation id="%s" codecs="%s" bandwidth="%d"`, kind, t.Codec, t.Bandwidth)
	if kind == "video" {
		fmt.Fprintf(b, ` width="%d" height="%d" frameRate="%g"`, t.Width, t.Height, t.FPS)
		b.WriteString(">\n")
	} else {
		fmt.Fprintf(b, ` audioSamplingRate="%d">`, t.Timescale)
		b.WriteString("\n")
		fmt.Fprintf(b, `        <AudioChannelConfiguration schemeIdUri="urn:mpeg:dash:23003:3:audio_channel_configuration:2011" value="%d"/>`+"\n", t.Channels)
	}
	windowStart := time.Now().Add(-window)
	idx := 0
	for i, c := range chunks {
		if c.From.After(windowStart) {
			idx = i
			break
		}
	}
	first := chunks[idx]
	fmt.Fprintf(b, `        <SegmentTemplate timescale="%d" initialization="init-%s.mp4" media="%s-$Number$.m4s" startNumber="%d">`+"\n",
		t.Timescale, kind, kind, first.Seq+1)
	b.WriteString("          <SegmentTimeline>\n")
	for _, c := range chunks[idx:] {
		d := uint32(c.Duration.Seconds() * float64(t.Timescale))
		if d == 0 {
			d = 1
		}
		fmt.Fprintf(b, `            <S t="%d" d="%d"/>`+"\n", c.Start, d)
	}
	b.WriteString("          </SegmentTimeline>\n        </SegmentTemplate>\n      </Representation>\n    </AdaptationSet>\n")
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
