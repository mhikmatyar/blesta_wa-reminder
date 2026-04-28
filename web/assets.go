package webassets

import "embed"

// FS embeds the admin web assets so deployment only needs one binary.
//
//go:embed admin/index.html admin/app.js admin/styles.css
var FS embed.FS
