package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func ValidateRepository(root string) (ValidationResult, error) {
	result := ValidationResult{}

	validateLayout(root, &result)

	schemas, err := LoadSchemas(root)
	if err != nil {
		result.Add(ValidationIssue{Stage: "schema", Message: err.Error()})
		return result, nil
	}
	constraints, err := LoadConstraints(root)
	if err != nil {
		result.Add(ValidationIssue{Stage: "constraints", Path: "config/constraints.json", Message: err.Error()})
		return result, nil
	}

	objectsByType, parseIssues := loadObjectsWithIssues(root)
	for _, issue := range parseIssues {
		result.Add(issue)
	}

	for typeName, objects := range objectsByType {
		schema, ok := schemas[typeName]
		if !ok {
			result.Add(ValidationIssue{Stage: "schema", Path: filepath.ToSlash(filepath.Join("data", typeName)), Message: "missing schema file config/schemas/" + typeName + ".schema.json"})
			continue
		}
		for _, obj := range objects {
			validateObjectInvariants(obj, &result)
			validateObjectSchema(obj, schema, &result)
		}
	}
	for schemaType := range schemas {
		if _, ok := objectsByType[schemaType]; !ok {
			continue
		}
	}

	validateConstraints(objectsByType, constraints, &result)
	return result, nil
}

func validateLayout(root string, result *ValidationResult) {
	dataDir := filepath.Join(root, "data")
	if st, err := os.Stat(dataDir); err != nil || !st.IsDir() {
		result.Add(ValidationIssue{Stage: "layout", Path: "data", Message: "missing data directory"})
	} else {
		entries, _ := os.ReadDir(dataDir)
		for _, typeEntry := range entries {
			typePath := filepath.Join(dataDir, typeEntry.Name())
			rel, _ := filepath.Rel(root, typePath)
			rel = filepath.ToSlash(rel)
			if !typeEntry.IsDir() {
				result.Add(ValidationIssue{Stage: "layout", Path: rel, Message: "only type directories are allowed directly under data/"})
				continue
			}
			files, _ := os.ReadDir(typePath)
			for _, f := range files {
				fp := filepath.Join(typePath, f.Name())
				relFile, _ := filepath.Rel(root, fp)
				relFile = filepath.ToSlash(relFile)
				if f.IsDir() {
					result.Add(ValidationIssue{Stage: "layout", Path: relFile, Message: "nested directories under data/<type>/ are not allowed"})
					continue
				}
				if !strings.HasSuffix(f.Name(), ".yaml") {
					result.Add(ValidationIssue{Stage: "layout", Path: relFile, Message: "only .yaml files are allowed in data/<type>/"})
					continue
				}
				id := strings.TrimSuffix(f.Name(), ".yaml")
				if !uuidPattern.MatchString(id) {
					result.Add(ValidationIssue{Stage: "layout", Path: relFile, Message: "filename must be a UUID"})
				}
			}
		}
	}

	configDir := filepath.Join(root, "config")
	if st, err := os.Stat(configDir); err != nil || !st.IsDir() {
		result.Add(ValidationIssue{Stage: "layout", Path: "config", Message: "missing config directory"})
	} else {
		entries, _ := os.ReadDir(configDir)
		for _, entry := range entries {
			switch {
			case entry.IsDir() && entry.Name() == "schemas":
				validateSchemaLayout(root, result)
			case !entry.IsDir() && entry.Name() == "constraints.json":
			default:
				p := filepath.ToSlash(filepath.Join("config", entry.Name()))
				result.Add(ValidationIssue{Stage: "layout", Path: p, Message: "file is not allowed under config/"})
			}
		}
	}
}

func validateSchemaLayout(root string, result *ValidationResult) {
	schemaDir := filepath.Join(root, "config", "schemas")
	entries, err := os.ReadDir(schemaDir)
	if err != nil {
		result.Add(ValidationIssue{Stage: "layout", Path: "config/schemas", Message: "cannot read schemas directory"})
		return
	}
	for _, entry := range entries {
		p := filepath.ToSlash(filepath.Join("config", "schemas", entry.Name()))
		if entry.IsDir() {
			result.Add(ValidationIssue{Stage: "layout", Path: p, Message: "nested directories are not allowed in config/schemas"})
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".schema.json") {
			result.Add(ValidationIssue{Stage: "layout", Path: p, Message: "schema filename must end with .schema.json"})
		}
	}
}

func loadObjectsWithIssues(root string) (map[string][]Object, []ValidationIssue) {
	issues := make([]ValidationIssue, 0)
	objects := make(map[string][]Object)

	dataDir := filepath.Join(root, "data")
	types, err := os.ReadDir(dataDir)
	if err != nil {
		issues = append(issues, ValidationIssue{Stage: "parse", Path: "data", Message: err.Error()})
		return objects, issues
	}

	for _, typeEntry := range types {
		if !typeEntry.IsDir() {
			continue
		}
		typeName := typeEntry.Name()
		typeDir := filepath.Join(dataDir, typeName)
		files, err := os.ReadDir(typeDir)
		if err != nil {
			issues = append(issues, ValidationIssue{Stage: "parse", Path: filepath.ToSlash(filepath.Join("data", typeName)), Message: err.Error()})
			continue
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".yaml") {
				continue
			}
			id := strings.TrimSuffix(file.Name(), ".yaml")
			path := filepath.Join(typeDir, file.Name())
			obj, err := ParseObjectFile(path, typeName, id)
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if err != nil {
				issues = append(issues, ValidationIssue{Stage: "parse", Path: rel, Message: err.Error()})
				continue
			}
			obj.Path = rel
			objects[typeName] = append(objects[typeName], obj)
		}
		sort.Slice(objects[typeName], func(i, j int) bool {
			return objects[typeName][i].ID < objects[typeName][j].ID
		})
	}
	return objects, issues
}

func validateObjectInvariants(obj Object, result *ValidationResult) {
	if !uuidPattern.MatchString(obj.ID) {
		result.Add(ValidationIssue{Stage: "parse", Path: obj.Path, Field: "_id", Message: "must be a UUID"})
	}
	if obj.Type == "" {
		result.Add(ValidationIssue{Stage: "parse", Path: obj.Path, Field: "_type", Message: "must be non-empty"})
	}
	for field, v := range obj.Data {
		switch t := v.(type) {
		case map[string]any:
			_ = t
			result.Add(ValidationIssue{Stage: "parse", Path: obj.Path, Field: field, Message: "nested objects are not supported in v1"})
		case []any:
			for _, item := range t {
				switch item.(type) {
				case string, float64:
				default:
					result.Add(ValidationIssue{Stage: "parse", Path: obj.Path, Field: field, Message: "arrays may contain only strings or numbers"})
				}
			}
		}
	}
}

func validateObjectSchema(obj Object, schema Schema, result *ValidationResult) {
	for req := range schema.Required {
		v, ok := obj.Data[req]
		if !ok || v == nil {
			result.Add(ValidationIssue{Stage: "schema", Path: obj.Path, Field: req, Message: "required field is missing"})
		}
	}

	for field, value := range obj.Data {
		if field == "_id" || field == "_type" {
			continue
		}
		prop, ok := schema.Properties[field]
		if !ok {
			result.Add(ValidationIssue{Stage: "schema", Path: obj.Path, Field: field, Message: "field is not defined in schema"})
			continue
		}
		validateProperty(field, value, prop, obj.Path, result)
	}
}

func validateProperty(field string, value any, prop SchemaProperty, path string, result *ValidationResult) {
	if value == nil {
		return
	}
	switch prop.Type {
	case "string":
		s, ok := value.(string)
		if !ok {
			result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: "must be a string"})
			return
		}
		if prop.MinLength != nil && len(s) < *prop.MinLength {
			result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: fmt.Sprintf("length must be >= %d", *prop.MinLength)})
		}
		if prop.MaxLength != nil && len(s) > *prop.MaxLength {
			result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: fmt.Sprintf("length must be <= %d", *prop.MaxLength)})
		}
		if len(prop.Enum) > 0 {
			matched := false
			for _, e := range prop.Enum {
				if s == e {
					matched = true
					break
				}
			}
			if !matched {
				result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: "value must be one of enum values"})
			}
		}
	case "number", "integer":
		n, ok := value.(float64)
		if !ok {
			result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: "must be a number"})
			return
		}
		if prop.Type == "integer" && n != float64(int64(n)) {
			result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: "must be an integer"})
		}
		if prop.Minimum != nil && n < *prop.Minimum {
			result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: fmt.Sprintf("must be >= %g", *prop.Minimum)})
		}
		if prop.Maximum != nil && n > *prop.Maximum {
			result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: fmt.Sprintf("must be <= %g", *prop.Maximum)})
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: "must be a boolean"})
		}
	case "array":
		arr, ok := value.([]any)
		if !ok {
			result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: "must be an array"})
			return
		}
		for _, item := range arr {
			switch prop.ItemsType {
			case "string":
				if _, ok := item.(string); !ok {
					result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: "array items must be strings"})
				}
			case "number", "integer":
				n, ok := item.(float64)
				if !ok {
					result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: "array items must be numbers"})
					continue
				}
				if prop.ItemsType == "integer" && n != float64(int64(n)) {
					result.Add(ValidationIssue{Stage: "schema", Path: path, Field: field, Message: "array items must be integers"})
				}
			}
		}
	}
}

func validateConstraints(objects map[string][]Object, constraints Constraints, result *ValidationResult) {
	for _, c := range constraints.Unique {
		seen := map[string]string{}
		for _, obj := range objects[c.Type] {
			v, ok := obj.Data[c.Field]
			if !ok || v == nil {
				continue
			}
			key := constraintValueKey(v)
			if key == "" {
				result.Add(ValidationIssue{Stage: "constraints", Path: obj.Path, Field: c.Field, Message: "unique constraint requires scalar field"})
				continue
			}
			if prev, ok := seen[key]; ok {
				result.Add(ValidationIssue{Stage: "constraints", Path: obj.Path, Field: c.Field, Message: fmt.Sprintf("duplicate value also used by %s", prev)})
			} else {
				seen[key] = obj.Path
			}
		}
	}

	for _, fk := range constraints.ForeignKeys {
		targets := map[string]struct{}{}
		for _, target := range objects[fk.ToType] {
			v, ok := target.Data[fk.ToField]
			if !ok || v == nil {
				continue
			}
			k := constraintValueKey(v)
			if k != "" {
				targets[k] = struct{}{}
			}
		}
		for _, source := range objects[fk.FromType] {
			v, ok := source.Data[fk.FromField]
			if !ok || v == nil {
				continue
			}
			k := constraintValueKey(v)
			if k == "" {
				result.Add(ValidationIssue{Stage: "constraints", Path: source.Path, Field: fk.FromField, Message: "foreign key must be a scalar value"})
				continue
			}
			if _, ok := targets[k]; !ok {
				result.Add(ValidationIssue{Stage: "constraints", Path: source.Path, Field: fk.FromField, Message: fmt.Sprintf("reference does not exist in %s.%s", fk.ToType, fk.ToField)})
			}
		}
	}
}

func constraintValueKey(v any) string {
	switch t := v.(type) {
	case string:
		return "s:" + t
	case float64:
		return "n:" + formatNumber(t)
	case bool:
		if t {
			return "b:true"
		}
		return "b:false"
	default:
		return ""
	}
}
