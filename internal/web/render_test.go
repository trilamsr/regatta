package web

import (
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// B-tier #5 — malformed template fails LoadTemplates loud at boot.
func TestLoadTemplates_FailsBootOnMalformedTemplate(t *testing.T) {
	bad := fstest.MapFS{
		"templates/broken.tmpl": &fstest.MapFile{Data: []byte("{{ .Unclosed ")},
	}
	if _, err := LoadTemplates(bad); err == nil {
		t.Fatal("LoadTemplates err=nil; expected parse failure on malformed template")
	}
}

// A-tier #7 — html/template auto-escapes user-supplied content.
func TestRenderTemplates_AutoEscapesUserInput(t *testing.T) {
	tmpls, err := LoadTemplates(assetsFS)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	rec := httptest.NewRecorder()
	data := struct{ UserContent string }{UserContent: "<script>alert(1)</script>"}
	if err := tmpls.Render(rec, "_flash.tmpl", data); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("auto-escape bypass: raw <script> survived in body=%q", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("expected escaped &lt;script&gt; substring in body=%q", body)
	}
}

// Guards the seam contract — RegisterFunc rejects collisions per spec §3.4.
func TestTemplates_RegisterFunc_RejectsDuplicate(t *testing.T) {
	tmpls, err := LoadTemplates(assetsFS)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	if err := tmpls.RegisterFunc("formatTime", func() string { return "" }); err == nil {
		t.Error("RegisterFunc accepted duplicate name; expected ErrFuncExists")
	}
	if err := tmpls.RegisterFunc("freshName", func() string { return "" }); err != nil {
		t.Errorf("RegisterFunc rejected fresh name: %v", err)
	}
}

// TestRender_BuffersBeforeWrite pins the contract that a template execution error returns the error WITHOUT having written any bytes to the http.ResponseWriter — closes the operator-dashboard log noise where serveAgentDrawer called http.Error after Render had already emitted a 200 OK.
func TestRender_BuffersBeforeWrite(t *testing.T) {
	const tmpl = "{{define \"flaky\"}}{{.MustExplode}}{{end}}"
	parsed := template.Must(template.New("layout").Parse(tmpl))
	tt := &Templates{parsed: parsed}
	rec := httptest.NewRecorder()
	if err := tt.Render(rec, "flaky", struct{}{}); err == nil {
		t.Fatalf("Render should propagate the template error")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("Render must not write bytes on failure; got %d", rec.Body.Len())
	}
}
