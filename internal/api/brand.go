package api

import (
	"embed"
	"encoding/base64"
	"io/fs"
)

//go:embed brand/*
var brandFS embed.FS

func brandB64(name string) string {
	data, err := fs.ReadFile(brandFS, "brand/"+name)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func brandFavicons() map[string]string {
	out := map[string]string{}
	for _, size := range []string{"16", "32", "48", "64", "128"} {
		if v := brandB64("favicons/" + size + ".png"); v != "" {
			out[size] = v
		}
	}
	return out
}
