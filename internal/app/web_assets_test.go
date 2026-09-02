package app

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestPageTemplatesLoadHTMX4FromUnpkgWithIntegrity(t *testing.T) {
	tmpl, err := template.ParseFS(webAssets, "templates/*.html")
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}

	const (
		htmxSource    = `src="https://unpkg.com/htmx.org@4.0.0/dist/htmx.min.js"`
		htmxIntegrity = `integrity="sha384-BvJpBiO8Kh31EqtJe5DRIeWrHWnCGkwytKs9NKFi86Hhw96dEqdEMzZDeK9iEGTc"`
		crossOrigin   = `crossorigin="anonymous"`
	)

	pages := map[string]any{
		"config.html":            configPageData{},
		"object.html":            objectPageData{},
		"promote_conflicts.html": conflictView{},
		"type.html":              typePageData{},
		"type_config.html":       typeConfigPageData{},
		"types.html":             typesPageData{},
		"workspace_new.html":     workspaceNewPageData{},
	}

	for name, data := range pages {
		t.Run(name, func(t *testing.T) {
			var rendered bytes.Buffer
			if err := tmpl.ExecuteTemplate(&rendered, name, data); err != nil {
				t.Fatalf("render template: %v", err)
			}

			html := rendered.String()
			for _, want := range []string{htmxSource, htmxIntegrity, crossOrigin} {
				if !strings.Contains(html, want) {
					t.Errorf("rendered template does not contain %s", want)
				}
			}
		})
	}
}
