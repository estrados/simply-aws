package web

import "embed"

//go:embed styles.css
var Static embed.FS

//go:embed templates/*.html
var Templates embed.FS
