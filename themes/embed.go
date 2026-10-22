package themes

import "embed"

//go:embed default/*.html compact/*.html
var FS embed.FS
