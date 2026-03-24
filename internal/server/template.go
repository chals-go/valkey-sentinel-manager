package server

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// monitoringPages defines pages that show the auto-refresh control.
var monitoringPages = map[string]bool{
	"dashboard": true,
	"clusters":  true,
	"sentinels": true,
	"events":    true,
}

// TemplateFuncMap returns the default FuncMap for all templates.
func TemplateFuncMap(t func(string) string) template.FuncMap {
	return template.FuncMap{
		"t": t,
		"isMonitoringPage": func(page string) bool {
			return monitoringPages[page]
		},
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"datetime": func(ts float64) string {
			if ts == 0 {
				return "-"
			}
			return time.Unix(int64(ts), 0).Format("2006-01-02 15:04:05")
		},
		"upper": strings.ToUpper,
		"concat": func(parts ...string) string { return strings.Join(parts, "") },
		"sprintf": fmt.Sprintf,
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"maskValue": func(key, value string) string {
			if strings.Contains(key, "key") || strings.Contains(key, "secret") || strings.Contains(key, "password") {
				if value != "" {
					return "***"
				}
			}
			return value
		},
		"contains": strings.Contains,
		"sliceContains": func(slice []string, val string) bool {
			for _, s := range slice {
				if s == val {
					return true
				}
			}
			return false
		},
		"seq": func(start, end int) []int {
			var s []int
			for i := start; i <= end; i++ {
				s = append(s, i)
			}
			return s
		},
		"mapLen": func(m any) int {
			switch v := m.(type) {
			case map[string]any:
				return len(v)
			case map[string]string:
				return len(v)
			default:
				return 0
			}
		},
	}
}

// ParseTemplates parses all HTML templates from the given filesystem.
func ParseTemplates(fsys fs.FS, funcMap template.FuncMap) (*template.Template, error) {
	return template.New("").Funcs(funcMap).ParseFS(fsys, "web/templates/*.html")
}

// RenderTemplate writes a named template to the response.
func RenderTemplate(w http.ResponseWriter, tmpl *template.Template, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
