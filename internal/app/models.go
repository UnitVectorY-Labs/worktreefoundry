package app

import (
	"fmt"
	"sort"
)

type ValidationResult struct {
	Issues []ValidationIssue
}

func (r *ValidationResult) Add(issue ValidationIssue) {
	r.Issues = append(r.Issues, issue)
}

func (r ValidationResult) OK() bool {
	return len(r.Issues) == 0
}

type ValidationIssue struct {
	Stage   string
	Path    string
	Field   string
	Message string
}

func (i ValidationIssue) String() string {
	if i.Field != "" {
		return fmt.Sprintf("[%s] %s (%s): %s", i.Stage, i.Path, i.Field, i.Message)
	}
	if i.Path != "" {
		return fmt.Sprintf("[%s] %s: %s", i.Stage, i.Path, i.Message)
	}
	return fmt.Sprintf("[%s] %s", i.Stage, i.Message)
}

type Schema struct {
	Type       string
	Required   map[string]struct{}
	Properties map[string]SchemaProperty
}

type SchemaProperty struct {
	Type      string
	Enum      []string
	MinLength *int
	MaxLength *int
	Minimum   *float64
	Maximum   *float64
	ItemsType string
}

type Constraints struct {
	Unique      []UniqueConstraint     `json:"unique"`
	ForeignKeys []ForeignKeyConstraint `json:"foreignKeys"`
}

type UniqueConstraint struct {
	Type  string `json:"type"`
	Field string `json:"field"`
}

type ForeignKeyConstraint struct {
	FromType       string `json:"fromType"`
	FromField      string `json:"fromField"`
	ToType         string `json:"toType"`
	ToField        string `json:"toField"`
	ToDisplayField string `json:"toDisplayField,omitempty"`
}

type Object struct {
	ID       string
	Type     string
	Data     map[string]any
	Path     string
	Deleted  bool
	Modified bool
}

type RepositoryState struct {
	Schemas     map[string]Schema
	ObjectsByTy map[string][]Object
	Constraints Constraints
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
