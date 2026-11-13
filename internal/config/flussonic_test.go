package config

import (
	"strings"
	"testing"
)

const flussonicSample = `
# Global settings:
http 8081;
rtmp 1935;
session_log /var/lib/flussonic;
edit_auth mdshahin SH01833055459@@%;

ondemand 376 {
  disabled false;
  input tshttp://live.example.com/live/a/92197.ts;
  input hlss://601.example.com/wwe.m3u8;
  input hls://193.37.41.71:5220/api/stream?id=550&1.m3u8;
  title "WWE Network";
  thumbnails;
  drm aes128 keyserver=https://keys.example.com/serverkey.php;
  transcoder vb=copy ab=128k acodec=aac;
  epg on;
  push push_a1ddc {
    url rtmps://live.example.com:443/live/key;
  }
}

stream 114 {
  disabled true;
  input tshttp://cdn.example.com/live/1055031.ts;
  title TSN 4;
}

dvr main {
  root /var/lib/flussonic/dvr;
  expiration 604800;
}
`

func TestParseFlussonicSyntax(t *testing.T) {
	c, err := ParseText(flussonicSample)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.HTTP.Port != 8081 {
		t.Fatalf("http port = %d, want 8081", c.HTTP.Port)
	}
	if c.AdminLogin != "mdshahin" || c.AdminPassword != "SH01833055459@@%" {
		t.Fatalf("edit_auth = %q/%q", c.AdminLogin, c.AdminPassword)
	}
	if len(c.Streams) != 2 {
		t.Fatalf("streams = %d, want 2", len(c.Streams))
	}

	s := c.Streams["376"]
	if s == nil {
		t.Fatal("ondemand 376 missing")
	}
	if !s.Ondemand || s.Keyword != "ondemand" {
		t.Fatalf("376 keyword = %q ondemand = %v", s.Keyword, s.Ondemand)
	}
	if s.Title != "WWE Network" {
		t.Fatalf("376 title = %q", s.Title)
	}
	if len(s.Inputs) != 3 {
		t.Fatalf("376 inputs = %d, want 3", len(s.Inputs))
	}
	want := []struct{ typ, url string }{
		{"tshttp", "http://live.example.com/live/a/92197.ts"},
		{"hls", "https://601.example.com/wwe.m3u8"},
		{"hls", "http://193.37.41.71:5220/api/stream?id=550&1.m3u8"},
	}
	for i, w := range want {
		if s.Inputs[i].Type != w.typ || s.Inputs[i].URL != w.url {
			t.Fatalf("input %d = %q %q, want %q %q", i, s.Inputs[i].Type, s.Inputs[i].URL, w.typ, w.url)
		}
	}
	if len(s.Pushes) != 1 || s.Pushes[0].Name != "push_a1ddc" || !strings.HasPrefix(s.Pushes[0].URL, "rtmps://") {
		t.Fatalf("push = %+v", s.Pushes)
	}
	names := map[string]bool{}
	for _, e := range s.Extras {
		names[e.Name] = true
	}
	for _, n := range []string{"thumbnails", "drm", "transcoder", "epg"} {
		if !names[n] {
			t.Fatalf("extra %q was dropped", n)
		}
	}

	d := c.Streams["114"]
	if d == nil || !d.Disabled {
		t.Fatal("stream 114 should be disabled")
	}
	if err := Validate(c); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestRenderRoundTrip(t *testing.T) {
	c, err := ParseText(flussonicSample)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	text := c.RenderText()
	again, err := ParseText(text)
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, text)
	}
	if !strings.Contains(text, "ondemand 376 {") {
		t.Fatalf("ondemand keyword lost:\n%s", text)
	}
	if !strings.Contains(text, "input tshttp://live.example.com/live/a/92197.ts;") {
		t.Fatalf("tshttp scheme lost:\n%s", text)
	}
	if !strings.Contains(text, "input hlss://601.example.com/wwe.m3u8;") {
		t.Fatalf("hlss scheme lost:\n%s", text)
	}
	if !strings.Contains(text, "push push_a1ddc {") {
		t.Fatalf("push block lost:\n%s", text)
	}
	if !strings.Contains(text, "transcoder vb=copy ab=128k acodec=aac;") {
		t.Fatalf("transcoder lost:\n%s", text)
	}
	if strings.Contains(text, "ondemand true;") {
		t.Fatalf("redundant ondemand flag:\n%s", text)
	}
	for _, name := range []string{"376", "114"} {
		a, b := c.Streams[name], again.Streams[name]
		if a == nil || b == nil {
			t.Fatalf("stream %q lost", name)
		}
		if a.Title != b.Title || a.Ondemand != b.Ondemand || a.Disabled != b.Disabled || len(a.Inputs) != len(b.Inputs) {
			t.Fatalf("stream %q changed across round trip", name)
		}
	}
}
