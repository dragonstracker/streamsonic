package api

import (
	"compress/gzip"
	"net/http"
	"path"
	"strings"
)

var compressedExt = map[string]bool{
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".ico": true,
	".mp4": true, ".m4s": true, ".ts": true, ".mp3": true, ".aac": true, ".mpd": true, ".m3u8": true,
	".zip": true, ".gz": true, ".br": true, ".zst": true,
}

type gzipWriter struct {
	http.ResponseWriter
	zw *gzip.Writer
}

func (w *gzipWriter) WriteHeader(status int) {
	w.Header().Del("Content-Length")
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipWriter) Write(b []byte) (int, error) {
	w.Header().Del("Content-Length")
	return w.zw.Write(b)
}

func (w *gzipWriter) Flush() {
	w.zw.Flush()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func compressible(r *http.Request) bool {
	if r.Method == http.MethodHead || r.Header.Get("Range") != "" {
		return false
	}
	if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		return false
	}
	p := r.URL.Path
	if strings.HasPrefix(p, "/stream/") {
		return false
	}
	if _, file, ok := strings.Cut(strings.TrimPrefix(p, "/"), "/"); ok && looksLikeMedia(file) {
		return false
	}
	if compressedExt[strings.ToLower(path.Ext(p))] {
		return false
	}
	return true
}

func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !compressible(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		zw := gzip.NewWriter(w)
		defer zw.Close()
		next.ServeHTTP(&gzipWriter{ResponseWriter: w, zw: zw}, r)
	})
}
