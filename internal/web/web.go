package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:web/dist
var assets embed.FS

func Handler() http.Handler {
	dist, err := fs.Sub(assets, "web/dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		files.ServeHTTP(w, r)
	})
}
