package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func ExportRepository(root, outDir string) error {
	result, err := ValidateRepository(root)
	if err != nil {
		return err
	}
	if !result.OK() {
		return fmt.Errorf("cannot export invalid repository: %s", result.Issues[0].String())
	}

	schemas, err := LoadSchemas(root)
	if err != nil {
		return err
	}
	objectsByType, err := LoadObjects(root)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	types := make([]string, 0, len(schemas))
	for t := range schemas {
		types = append(types, t)
	}
	sort.Strings(types)

	for _, t := range types {
		objs := objectsByType[t]
		sort.Slice(objs, func(i, j int) bool {
			return objs[i].ID < objs[j].ID
		})
		rows := make([]map[string]any, 0, len(objs))
		for _, obj := range objs {
			row := make(map[string]any, len(obj.Data))
			for k, v := range obj.Data {
				if k == "_id" || k == "_type" {
					continue
				}
				row[k] = v
			}
			rows = append(rows, row)
		}
		b, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		b = append(b, '\n')
		if err := os.WriteFile(filepath.Join(outDir, t+".json"), b, 0o644); err != nil {
			return err
		}
	}

	return nil
}
