package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed assets
var assets embed.FS

func content() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}

func Handler() http.Handler {
	root := content()
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/admin":
			http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
			return
		case strings.HasPrefix(path, "/admin/assets/"):
			serveAsset(w, r, fileServer, strings.TrimPrefix(path, "/admin/assets/"))
			return
		case strings.HasPrefix(path, "/admin/ui/"):
			serveAsset(w, r, fileServer, strings.TrimPrefix(path, "/admin/ui/"))
			return
		case strings.HasPrefix(path, "/ui/"):
			serveAsset(w, r, fileServer, strings.TrimPrefix(path, "/ui/"))
			return
		case strings.HasPrefix(path, "/assets/"):
			serveAsset(w, r, fileServer, strings.TrimPrefix(path, "/assets/"))
			return
		case path == "/admin/" || path == "/" || path == "/index.html":
			serveIndex(w, root)
			return
		}
		serveIndex(w, root)
	})
}

func serveAsset(w http.ResponseWriter, r *http.Request, fileServer http.Handler, name string) {
	if name == "" || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	if f, err := content().Open(name); err != nil {
		http.NotFound(w, r)
		return
	} else {
		f.Close()
	}
	r.URL.Path = "/" + name
	w.Header().Set("Cache-Control", "public, max-age=3600")
	fileServer.ServeHTTP(w, r)
}

func serveIndex(w http.ResponseWriter, root fs.FS) {
	data, err := fs.ReadFile(root, "index.html")
	if err != nil {
		http.Error(w, "ui not built", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}
