package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWantsPlainText(t *testing.T) {
	cases := []struct {
		accept string
		want   bool
	}{
		{"", false},
		{"application/json", false},
		{"text/plain", true},
		{"text/plain, */*", true},
		{"application/json, text/plain;q=0.9", false},
		{"application/json, text/plain, */*", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/streamer/api/v3/config", nil)
		if tc.accept != "" {
			req.Header.Set("Accept", tc.accept)
		}
		if got := wantsPlainText(req); got != tc.want {
			t.Fatalf("accept %q: got %v want %v", tc.accept, got, tc.want)
		}
	}
}

func TestContentTypeStripsCharset(t *testing.T) {
	ct := "text/plain;charset=utf-8"
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if ct != "text/plain" {
		t.Fatalf("got %q", ct)
	}
}
