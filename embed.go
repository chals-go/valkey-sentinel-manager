// Package vsm provides embedded filesystem access for templates and static files.
package vsm

import "embed"

//go:embed web/templates/*.html
var TemplateFS embed.FS

//go:embed web/static
var StaticFS embed.FS
