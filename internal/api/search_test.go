package api

import (
	"strings"
	"testing"
)

func TestTextSearchMatchesTitleSubstring(t *testing.T) {
	hay := strings.ToLower("1047143\nEFL - PORTSMOUTH\ntshttp://stream.mrxtx.one/live.ts")
	if !strings.Contains(hay, "tsm") {
		t.Fatal("expected portsmouth to contain tsm")
	}
	if !strings.Contains(hay, "1047143") {
		t.Fatal("expected name hit")
	}
	if strings.Contains(hay, "wwe") {
		t.Fatal("unexpected wwe hit")
	}
}

func TestProjectStream(t *testing.T) {
	view := map[string]any{"name": "a", "title": "b", "stats": map[string]any{"alive": false}}
	got := projectStream(view, "name,title")
	if len(got) != 2 || got["name"] != "a" || got["title"] != "b" {
		t.Fatalf("got %#v", got)
	}
	if projectStream(view, "")["stats"] == nil {
		t.Fatal("empty select should keep full view")
	}
}
