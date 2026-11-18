package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONConfigRejectsAmbiguousInput(t *testing.T) {
	readers := []struct {
		name string
		path string
		read func(string) error
	}{
		{"constraints", "config/constraints.json", func(root string) error { _, err := LoadConstraints(root); return err }},
		{"ui", "config/ui.json", func(root string) error { _, err := LoadUIConfig(root, nil); return err }},
		{"schema", "config/schemas/test.schema.json", func(root string) error { _, err := LoadSchemas(root); return err }},
	}
	for _, reader := range readers {
		for _, input := range []struct{ name, value string }{
			{"duplicate names", `{"type":"object","type":"object"}`},
			{"invalid UTF-8", "{\"name\":\"\xff\"}"},
		} {
			t.Run(reader.name+"/"+input.name, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, reader.path)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(input.value), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := reader.read(root); err == nil {
					t.Fatal("expected invalid JSON to be rejected")
				}
			})
		}
	}
}

func TestJSONConfigFieldNamesAreCaseSensitive(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config", "ui.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"RepoName":"wrong case"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadUIConfig(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RepoName != filepath.Base(root) {
		t.Fatalf("unexpected repo name: %q", cfg.RepoName)
	}
}

func TestJSONOutputUsesV2DefaultsAndStableOrder(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "output.json")
	value := map[string]any{"z": []string(nil), "a": map[string]string(nil)}
	if err := writeJSONFile(path, value); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": {},\n  \"z\": []\n}\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	cfg := UIConfig{RepoName: "test", Types: map[string]TypeUIConfig{"z": {}, "a": {}}}
	if err := SaveUIConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(root, "config", "ui.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(got), `"a"`) > strings.Index(string(got), `"z"`) {
		t.Fatalf("types not sorted: %s", got)
	}
}
