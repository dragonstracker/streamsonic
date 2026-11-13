package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gzipHandler() http.Handler {
	return withGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"streams":[]}`)
	}))
}

func TestGzipCompressesAPI(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streams", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	gzipHandler().ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("content encoding = %q, want gzip", got)
	}
	if v := rec.Header().Get("Vary"); !strings.Contains(v, "Accept-Encoding") {
		t.Fatalf("vary = %q, want Accept-Encoding", v)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("body is not gzip: %v", err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != `{"streams":[]}` {
		t.Fatalf("body = %q", body)
	}
}

func TestGzipLeavesIdentityAlone(t *testing.T) {
	cases := []struct {
		name   string
		target string
		header map[string]string
	}{
		{"hls manifest", "/114/index.m3u8", nil},
		{"dash segment", "/114/preview-7.m4s", nil},
		{"preview manifest", "/114/preview.json", nil},
		{"media prefix", "/stream/114/index.m3u8", nil},
		{"byte range", APIPrefix + "/streams", map[string]string{"Range": "bytes=0-9"}},
		{"font", "/admin/assets/fonts/custom-400.woff2", nil},
		{"no accept encoding", APIPrefix + "/streams", map[string]string{"Accept-Encoding": "identity"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			req.Header.Set("Accept-Encoding", "gzip")
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			gzipHandler().ServeHTTP(rec, req)
			if got := rec.Header().Get("Content-Encoding"); got != "" {
				t.Fatalf("%s: content encoding = %q, want none", tc.target, got)
			}
		})
	}
}
