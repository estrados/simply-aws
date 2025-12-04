package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html app.js styles.css
var static embed.FS

func Handler() http.Handler {
	sub, _ := fs.Sub(static, ".")
	return http.FileServer(http.FS(sub))
}
