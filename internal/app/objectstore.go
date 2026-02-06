package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func LoadObjects(root string) (map[string][]Object, error) {
	dataDir := filepath.Join(root, "data")
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]Object{}, nil
		}
		return nil, err
	}

	objects := make(map[string][]Object)
	for _, typeEntry := range entries {
		if !typeEntry.IsDir() {
			continue
		}
		typeName := typeEntry.Name()
		typeDir := filepath.Join(dataDir, typeName)
		files, err := os.ReadDir(typeDir)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".yaml") {
				continue
			}
			id := strings.TrimSuffix(file.Name(), ".yaml")
			objPath := filepath.Join(typeDir, file.Name())
			obj, err := ParseObjectFile(objPath, typeName, id)
			if err != nil {
				return nil, err
			}
			obj.Path, _ = filepath.Rel(root, objPath)
			obj.Path = filepath.ToSlash(obj.Path)
			objects[typeName] = append(objects[typeName], obj)
		}
		sort.Slice(objects[typeName], func(i, j int) bool {
			return objects[typeName][i].ID < objects[typeName][j].ID
		})
	}
	return objects, nil
}

func ParseObjectFile(path, expectedType, expectedID string) (Object, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Object{}, err
	}
	m, err := ParseSimpleYAMLObject(b)
	if err != nil {
		return Object{}, fmt.Errorf("parse YAML: %w", err)
	}
	if len(m) == 0 {
		return Object{}, errors.New("YAML root must contain fields")
	}

	normalized := make(map[string]any, len(m))
	for k, v := range m {
		nv, err := normalizeObjectValue(v)
		if err != nil {
			return Object{}, fmt.Errorf("field %s: %w", k, err)
		}
		normalized[k] = nv
	}
	idVal, ok := normalized["_id"].(string)
	if !ok || idVal == "" {
		return Object{}, errors.New("missing _id string")
	}
	typeVal, ok := normalized["_type"].(string)
	if !ok || typeVal == "" {
		return Object{}, errors.New("missing _type string")
	}
	if expectedID != "" && idVal != expectedID {
		return Object{}, fmt.Errorf("_id %q does not match filename %q", idVal, expectedID)
	}
	if expectedType != "" && typeVal != expectedType {
		return Object{}, fmt.Errorf("_type %q does not match folder %q", typeVal, expectedType)
	}
	return Object{ID: idVal, Type: typeVal, Data: normalized, Path: path}, nil
}

func normalizeObjectValue(v any) (any, error) {
	switch t := v.(type) {
	case string, bool, nil:
		return t, nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case float64:
		return t, nil
	case []any:
		if len(t) == 0 {
			return []any{}, nil
		}
		result := make([]any, 0, len(t))
		elemKind := ""
		for _, item := range t {
			nv, err := normalizeObjectValue(item)
			if err != nil {
				return nil, err
			}
			switch nv.(type) {
			case string:
				if elemKind == "" {
					elemKind = "string"
				}
				if elemKind != "string" {
					return nil, errors.New("array elements must all be same primitive type")
				}
			case float64:
				if elemKind == "" {
					elemKind = "number"
				}
				if elemKind != "number" {
					return nil, errors.New("array elements must all be same primitive type")
				}
			default:
				return nil, errors.New("arrays may contain only strings or numbers")
			}
			result = append(result, nv)
		}
		return result, nil
	case map[string]any:
		return nil, errors.New("nested objects are not supported in v1")
	default:
		return nil, fmt.Errorf("unsupported value type %T", v)
	}
}

func WriteObject(repoRoot string, obj Object) error {
	if obj.ID == "" || obj.Type == "" {
		return errors.New("object missing id/type")
	}
	rel := filepath.Join("data", obj.Type, obj.ID+".yaml")
	abs := filepath.Join(repoRoot, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	b, err := CanonicalYAML(obj.Data)
	if err != nil {
		return err
	}
	return os.WriteFile(abs, b, 0o644)
}

func DeleteObject(repoRoot, typeName, id string) error {
	abs := filepath.Join(repoRoot, "data", typeName, id+".yaml")
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func ReadObject(repoRoot, typeName, id string) (Object, error) {
	path := filepath.Join(repoRoot, "data", typeName, id+".yaml")
	obj, err := ParseObjectFile(path, typeName, id)
	if err != nil {
		return Object{}, err
	}
	rel, _ := filepath.Rel(repoRoot, path)
	obj.Path = filepath.ToSlash(rel)
	return obj, nil
}

func CanonicalYAML(data map[string]any) ([]byte, error) {
	return MarshalSimpleYAMLObject(data)
}

func formatNumber(n float64) string {
	if n == float64(int64(n)) {
		return fmt.Sprintf("%d", int64(n))
	}
	return fmt.Sprintf("%g", n)
}

func RewriteCanonicalFiles(repoPath string, changed []string) error {
	for _, rel := range changed {
		if !strings.HasPrefix(rel, "data/") || !strings.HasSuffix(rel, ".yaml") {
			continue
		}
		abs := filepath.Join(repoPath, filepath.FromSlash(rel))
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		typeName := filepath.Base(filepath.Dir(abs))
		id := strings.TrimSuffix(filepath.Base(abs), ".yaml")
		obj, err := ParseObjectFile(abs, typeName, id)
		if err != nil {
			return fmt.Errorf("canonicalize %s: %w", rel, err)
		}
		b, err := CanonicalYAML(obj.Data)
		if err != nil {
			return err
		}
		if err := os.WriteFile(abs, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func ListObjectsForType(repoRoot, typeName string) ([]Object, error) {
	dir := filepath.Join(repoRoot, "data", typeName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	objs := make([]Object, 0)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".yaml")
		obj, err := ParseObjectFile(filepath.Join(dir, e.Name()), typeName, id)
		if err != nil {
			return nil, err
		}
		obj.Path = filepath.ToSlash(filepath.Join("data", typeName, e.Name()))
		objs = append(objs, obj)
	}
	sort.Slice(objs, func(i, j int) bool {
		return objs[i].ID < objs[j].ID
	})
	return objs, nil
}
