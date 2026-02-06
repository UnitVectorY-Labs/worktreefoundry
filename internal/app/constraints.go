package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func LoadConstraints(root string) (Constraints, error) {
	path := filepath.Join(root, "config", "constraints.json")
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Constraints{}, nil
		}
		return Constraints{}, err
	}
	var c Constraints
	if err := json.Unmarshal(b, &c); err != nil {
		return Constraints{}, fmt.Errorf("parse constraints: %w", err)
	}
	return c, nil
}
