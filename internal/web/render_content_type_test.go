package web

import (
	"html/template"
	"net/http/httptest"
	"testing"
)

// TestRender_SetsHTMLContentType pins R27 explicit Content-Type before any body write.
func TestRender_SetsHTMLContentType(t *testing.T) {
	const tmpl = `{{define "page"}}ok{{end}}`
	parsed := template.Must(template.New("layout").Parse(tmpl))
	tt := &Templates{parsed: parsed}
	rec := httptest.NewRecorder()
	if err := tt.Render(rec, "page", nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got, want := rec.Header().Get("Content-Type"), "text/html; charset=utf-8"; got != want {
		t.Fatalf("Content-Type=%q want %q", got, want)
	}
}

// TestRender_OverridesGoDefaultSniff pins R27 header override vs DetectContentType.
func TestRender_OverridesGoDefaultSniff(t *testing.T) {
	const tmpl = `{{define "tiny"}}hi{{end}}`
	parsed := template.Must(template.New("layout").Parse(tmpl))
	tt := &Templates{parsed: parsed}
	rec := httptest.NewRecorder()
	if err := tt.Render(rec, "tiny", nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got, want := rec.Header().Get("Content-Type"), "text/html; charset=utf-8"; got != want {
		t.Fatalf("Content-Type=%q want %q (sniff would yield text/plain on %q)", got, want, rec.Body.String())
	}
}
