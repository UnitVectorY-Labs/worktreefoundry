package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type UIConfig struct {
	RepoName string                  `json:"repoName"`
	Types    map[string]TypeUIConfig `json:"types"`
}

type TypeUIConfig struct {
	DisplayField string   `json:"displayField"`
	Fields       []string `json:"fields"`
}

func LoadUIConfig(root string, schemas map[string]Schema) (UIConfig, error) {
	cfg := DefaultUIConfig(root, schemas)
	path := filepath.Join(root, "config", "ui.json")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return UIConfig{}, err
	}
	var parsed UIConfig
	if err := json.Unmarshal(b, &parsed); err != nil {
		return UIConfig{}, fmt.Errorf("parse ui config: %w", err)
	}

	if strings.TrimSpace(parsed.RepoName) != "" {
		cfg.RepoName = strings.TrimSpace(parsed.RepoName)
	}
	if parsed.Types != nil {
		for typeName, tc := range parsed.Types {
			normalized := TypeUIConfig{DisplayField: tc.DisplayField, Fields: dedupeOrdered(tc.Fields)}
			if normalized.DisplayField == "" {
				normalized.DisplayField = "_id"
			}
			cfg.Types[typeName] = normalized
		}
	}
	return cfg, nil
}

func DefaultUIConfig(root string, schemas map[string]Schema) UIConfig {
	cfg := UIConfig{
		RepoName: filepath.Base(root),
		Types:    map[string]TypeUIConfig{},
	}
	types := make([]string, 0, len(schemas))
	for t := range schemas {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		cfg.Types[t] = TypeUIConfig{DisplayField: "_id", Fields: []string{}}
	}
	return cfg
}

func SaveUIConfig(root string, cfg UIConfig) error {
	if strings.TrimSpace(cfg.RepoName) == "" {
		return fmt.Errorf("repo name is required")
	}
	cfg.RepoName = strings.TrimSpace(cfg.RepoName)
	if cfg.Types == nil {
		cfg.Types = map[string]TypeUIConfig{}
	}

	types := make([]string, 0, len(cfg.Types))
	for t := range cfg.Types {
		types = append(types, t)
	}
	sort.Strings(types)
	normalized := UIConfig{RepoName: cfg.RepoName, Types: map[string]TypeUIConfig{}}
	for _, t := range types {
		tc := cfg.Types[t]
		if tc.DisplayField == "" {
			tc.DisplayField = "_id"
		}
		tc.Fields = dedupeOrdered(tc.Fields)
		normalized.Types[t] = tc
	}

	b, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	path := filepath.Join(root, "config", "ui.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func ValidateUIConfig(cfg UIConfig, schemas map[string]Schema) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if strings.TrimSpace(cfg.RepoName) == "" {
		issues = append(issues, ValidationIssue{Stage: "config", Path: "config/ui.json", Field: "repoName", Message: "repoName is required"})
	}
	for typeName, tc := range cfg.Types {
		schema, ok := schemas[typeName]
		if !ok {
			issues = append(issues, ValidationIssue{Stage: "config", Path: "config/ui.json", Field: "types." + typeName, Message: "unknown type"})
			continue
		}
		display := tc.DisplayField
		if display == "" {
			display = "_id"
		}
		if display != "_id" {
			if _, ok := schema.Properties[display]; !ok {
				issues = append(issues, ValidationIssue{Stage: "config", Path: "config/ui.json", Field: "types." + typeName + ".displayField", Message: "display field must exist in schema"})
			} else {
				if _, req := schema.Required[display]; !req {
					issues = append(issues, ValidationIssue{Stage: "config", Path: "config/ui.json", Field: "types." + typeName + ".displayField", Message: "display field must be required"})
				}
			}
		}
		seen := map[string]struct{}{}
		for _, field := range tc.Fields {
			if field == display {
				issues = append(issues, ValidationIssue{Stage: "config", Path: "config/ui.json", Field: "types." + typeName + ".fields", Message: "additional fields cannot include display field"})
				continue
			}
			if _, dup := seen[field]; dup {
				issues = append(issues, ValidationIssue{Stage: "config", Path: "config/ui.json", Field: "types." + typeName + ".fields", Message: "duplicate field in list"})
				continue
			}
			seen[field] = struct{}{}
			if _, ok := schema.Properties[field]; !ok {
				issues = append(issues, ValidationIssue{Stage: "config", Path: "config/ui.json", Field: "types." + typeName + ".fields", Message: "field " + field + " not in schema"})
			}
		}
	}
	return issues
}

func dedupeOrdered(fields []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}
