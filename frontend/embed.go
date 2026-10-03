package frontend

import "embed"

//go:embed "html/*" "static/*"
var Templates embed.FS
