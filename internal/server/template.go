package server

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// monitoringPages는 자동 새로고침 컨트롤을 표시하는 페이지 목록을 정의한다.
var monitoringPages = map[string]bool{
	"dashboard": true,
	"clusters":  true,
	"sentinels": true,
	"events":    true,
}

// TemplateFuncMap은 모든 템플릿에서 사용하는 기본 FuncMap을 반환한다.
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
		"csrfToken": func() string { return "" },
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

// ParseTemplates는 주어진 파일시스템에서 모든 HTML 템플릿을 파싱한다.
func ParseTemplates(fsys fs.FS, funcMap template.FuncMap) (*template.Template, error) {
	return template.New("").Funcs(funcMap).ParseFS(fsys, "web/templates/*.html")
}

// RenderTemplate은 지정한 이름의 템플릿을 HTTP 응답에 렌더링한다.
func RenderTemplate(w http.ResponseWriter, tmpl *template.Template, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
