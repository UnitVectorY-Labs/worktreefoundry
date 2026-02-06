package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type rawSchema struct {
	Type       string                   `json:"type"`
	Required   []string                 `json:"required"`
	Properties map[string]rawSchemaProp `json:"properties"`
}

type rawSchemaProp struct {
	Type      string    `json:"type"`
	Enum      []string  `json:"enum"`
	MinLength *int      `json:"minLength"`
	MaxLength *int      `json:"maxLength"`
	Minimum   *float64  `json:"minimum"`
	Maximum   *float64  `json:"maximum"`
	Items     *rawItems `json:"items"`
}

type rawItems struct {
	Type string `json:"type"`
}

func LoadSchemas(root string) (map[string]Schema, error) {
	schemaDir := filepath.Join(root, "config", "schemas")
	entries, err := os.ReadDir(schemaDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("missing schema directory: %s", schemaDir)
		}
		return nil, err
	}

	schemas := make(map[string]Schema)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		typeName := strings.TrimSuffix(entry.Name(), ".schema.json")
		b, err := os.ReadFile(filepath.Join(schemaDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var raw rawSchema
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("parse schema %s: %w", entry.Name(), err)
		}
		schema, err := normalizeSchema(typeName, raw)
		if err != nil {
			return nil, fmt.Errorf("schema %s: %w", entry.Name(), err)
		}
		schemas[typeName] = schema
	}
	if len(schemas) == 0 {
		return nil, fmt.Errorf("no schema files found in %s", schemaDir)
	}
	return schemas, nil
}

func normalizeSchema(typeName string, raw rawSchema) (Schema, error) {
	if raw.Type != "object" {
		return Schema{}, fmt.Errorf("root type must be object")
	}
	required := make(map[string]struct{}, len(raw.Required))
	for _, r := range raw.Required {
		required[r] = struct{}{}
	}
	props := make(map[string]SchemaProperty, len(raw.Properties))
	for field, p := range raw.Properties {
		sp := SchemaProperty{
			Type:      p.Type,
			Enum:      append([]string(nil), p.Enum...),
			MinLength: p.MinLength,
			MaxLength: p.MaxLength,
			Minimum:   p.Minimum,
			Maximum:   p.Maximum,
		}
		sort.Strings(sp.Enum)
		switch p.Type {
		case "string", "number", "integer", "boolean":
		case "array":
			if p.Items == nil {
				return Schema{}, fmt.Errorf("field %s: array missing items.type", field)
			}
			if p.Items.Type != "string" && p.Items.Type != "number" && p.Items.Type != "integer" {
				return Schema{}, fmt.Errorf("field %s: array items.type must be string/number/integer", field)
			}
			sp.ItemsType = p.Items.Type
		default:
			return Schema{}, fmt.Errorf("field %s: unsupported type %q", field, p.Type)
		}
		if p.Type == "array" && len(p.Enum) > 0 {
			return Schema{}, fmt.Errorf("field %s: enum not supported for array", field)
		}
		if p.Type != "string" && (p.MinLength != nil || p.MaxLength != nil) {
			return Schema{}, fmt.Errorf("field %s: minLength/maxLength only valid for string", field)
		}
		if p.Type != "number" && p.Type != "integer" && (p.Minimum != nil || p.Maximum != nil) {
			return Schema{}, fmt.Errorf("field %s: minimum/maximum only valid for number/integer", field)
		}
		props[field] = sp
	}
	if _, ok := props["_id"]; ok {
		return Schema{}, fmt.Errorf("_id must not appear in schema properties")
	}
	if _, ok := props["_type"]; ok {
		return Schema{}, fmt.Errorf("_type must not appear in schema properties")
	}
	return Schema{Type: typeName, Required: required, Properties: props}, nil
}
