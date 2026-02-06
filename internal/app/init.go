package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func InitializeRepository(root string, force bool, sample bool) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if st, err := os.Stat(abs); err == nil {
		if !st.IsDir() {
			return fmt.Errorf("path exists and is not a directory: %s", abs)
		}
		entries, _ := os.ReadDir(abs)
		if len(entries) > 0 && !force {
			return fmt.Errorf("directory is not empty: %s (use --force)", abs)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}

	if err := initGitRepo(abs); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(abs, "config", "schemas"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(abs, "data", "team"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(abs, "data", "service"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(abs, "output"), 0o755); err != nil {
		return err
	}

	if sample {
		if err := writeSampleSchemas(abs); err != nil {
			return err
		}
		if err := writeSampleConstraints(abs); err != nil {
			return err
		}
		if err := writeSampleObjects(abs); err != nil {
			return err
		}
	}
	schemas, err := LoadSchemas(abs)
	if err != nil && sample {
		return err
	}
	if err == nil {
		if err := SaveUIConfig(abs, DefaultUIConfig(abs, schemas)); err != nil {
			return err
		}
	}
	if err := ensureGitignoreDefaults(abs); err != nil {
		return err
	}

	if err := gitCommitAll(abs, "Initialize worktreefoundry repository"); err != nil {
		return err
	}
	return nil
}

func initGitRepo(root string) error {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return nil
	}
	if _, err := runCommand(root, "git", "init"); err != nil {
		return err
	}
	if _, err := runCommand(root, "git", "checkout", "-B", "main"); err != nil {
		return err
	}
	return nil
}

func writeSampleSchemas(root string) error {
	teamSchema := map[string]any{
		"type":     "object",
		"required": []string{"name", "code"},
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "minLength": 1},
			"code": map[string]any{"type": "string", "minLength": 2, "maxLength": 16},
		},
	}
	serviceSchema := map[string]any{
		"type":     "object",
		"required": []string{"name", "teamId", "tier"},
		"properties": map[string]any{
			"name":   map[string]any{"type": "string", "minLength": 1},
			"teamId": map[string]any{"type": "string"},
			"tier":   map[string]any{"type": "string", "enum": []string{"core", "edge", "batch"}},
			"ports":  map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
		},
	}
	if err := writeJSONFile(filepath.Join(root, "config", "schemas", "team.schema.json"), teamSchema); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(root, "config", "schemas", "service.schema.json"), serviceSchema); err != nil {
		return err
	}
	return nil
}

func writeSampleConstraints(root string) error {
	c := Constraints{
		Unique: []UniqueConstraint{
			{Type: "team", Field: "code"},
			{Type: "service", Field: "name"},
		},
		ForeignKeys: []ForeignKeyConstraint{
			{FromType: "service", FromField: "teamId", ToType: "team", ToField: "_id"},
		},
	}
	return writeJSONFile(filepath.Join(root, "config", "constraints.json"), c)
}

func writeSampleObjects(root string) error {
	teamID := "11111111-1111-4111-8111-111111111111"
	serviceID := "22222222-2222-4222-8222-222222222222"

	team := Object{
		ID:   teamID,
		Type: "team",
		Data: map[string]any{
			"_id":   teamID,
			"_type": "team",
			"name":  "Platform",
			"code":  "PLAT",
		},
	}
	service := Object{
		ID:   serviceID,
		Type: "service",
		Data: map[string]any{
			"_id":    serviceID,
			"_type":  "service",
			"name":   "edge-gateway",
			"teamId": teamID,
			"tier":   "edge",
			"ports":  []any{float64(443), float64(8443)},
		},
	}
	if err := WriteObject(root, team); err != nil {
		return err
	}
	if err := WriteObject(root, service); err != nil {
		return err
	}
	return nil
}

func ensureGitignoreDefaults(root string) error {
	path := filepath.Join(root, ".gitignore")
	content := ""
	if b, err := os.ReadFile(path); err == nil {
		content = string(b)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	if !containsGitignoreLine(content, "output/") {
		content += "output/\n"
	}
	if !containsGitignoreLine(content, ".worktreefoundry/") {
		content += ".worktreefoundry/\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func containsGitignoreLine(content, line string) bool {
	lines := strings.Split(content, "\n")
	for _, l := range lines {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func gitCommitAll(root, message string) error {
	if _, err := runCommand(root, "git", "add", "-A"); err != nil {
		return err
	}
	if _, err := runCommand(root, "git", "-c", "user.name=worktreefoundry", "-c", "user.email=worktreefoundry@local", "commit", "-m", message); err != nil {
		if strings.Contains(err.Error(), "nothing to commit") {
			return nil
		}
		return err
	}
	return nil
}

func runCommand(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
