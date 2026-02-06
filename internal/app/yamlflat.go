package app

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var numberLiteralPattern = regexp.MustCompile(`^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?$`)
var safeStringPattern = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

func ParseSimpleYAMLObject(input []byte) (map[string]any, error) {
	text := strings.ReplaceAll(string(input), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	out := make(map[string]any)

	for i := 0; i < len(lines); {
		line := strings.TrimRight(lines[i], " \t")
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") || strings.Contains(line, " #") {
			return nil, errors.New("comments are not allowed")
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			return nil, fmt.Errorf("unexpected indentation at line %d", i+1)
		}

		colon := strings.IndexRune(line, ':')
		if colon <= 0 {
			return nil, fmt.Errorf("line %d is not key: value", i+1)
		}
		key := strings.TrimSpace(line[:colon])
		rest := strings.TrimSpace(line[colon+1:])
		if key == "" {
			return nil, fmt.Errorf("line %d has empty key", i+1)
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("duplicate key %q", key)
		}

		if rest == "" {
			arr := make([]any, 0)
			i++
			for i < len(lines) {
				arrLine := strings.TrimRight(lines[i], " \t")
				if strings.TrimSpace(arrLine) == "" {
					i++
					continue
				}
				if strings.HasPrefix(strings.TrimSpace(arrLine), "#") || strings.Contains(arrLine, " #") {
					return nil, errors.New("comments are not allowed")
				}
				if !strings.HasPrefix(arrLine, "  - ") {
					break
				}
				itemRaw := strings.TrimSpace(strings.TrimPrefix(arrLine, "  - "))
				item, err := parseYAMLScalar(itemRaw)
				if err != nil {
					return nil, fmt.Errorf("line %d: %w", i+1, err)
				}
				arr = append(arr, item)
				i++
			}
			out[key] = arr
			continue
		}

		value, err := parseYAMLScalar(rest)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		out[key] = value
		i++
	}

	return out, nil
}

func parseYAMLScalar(raw string) (any, error) {
	if raw == "[]" {
		return []any{}, nil
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		s, err := strconv.Unquote(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid quoted string: %w", err)
		}
		return s, nil
	}
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1], nil
	}
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if numberLiteralPattern.MatchString(raw) {
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number: %w", err)
		}
		return n, nil
	}
	return raw, nil
}

func MarshalSimpleYAMLObject(data map[string]any) ([]byte, error) {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		v := data[key]
		switch t := v.(type) {
		case nil:
			fmt.Fprintf(&b, "%s: null\n", key)
		case string:
			fmt.Fprintf(&b, "%s: %s\n", key, renderYAMLString(t))
		case bool:
			if t {
				fmt.Fprintf(&b, "%s: true\n", key)
			} else {
				fmt.Fprintf(&b, "%s: false\n", key)
			}
		case float64:
			fmt.Fprintf(&b, "%s: %s\n", key, formatNumber(t))
		case []any:
			if len(t) == 0 {
				fmt.Fprintf(&b, "%s: []\n", key)
				continue
			}
			fmt.Fprintf(&b, "%s:\n", key)
			for _, item := range t {
				s, err := renderYAMLScalar(item)
				if err != nil {
					return nil, fmt.Errorf("field %s: %w", key, err)
				}
				fmt.Fprintf(&b, "  - %s\n", s)
			}
		default:
			return nil, fmt.Errorf("unsupported field %q type %T", key, v)
		}
	}
	return []byte(b.String()), nil
}

func renderYAMLScalar(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return renderYAMLString(t), nil
	case float64:
		return formatNumber(t), nil
	case int:
		return strconv.Itoa(t), nil
	case int64:
		return strconv.FormatInt(t, 10), nil
	case nil:
		return "null", nil
	default:
		return "", fmt.Errorf("unsupported scalar type %T", v)
	}
}

func renderYAMLString(s string) string {
	if s == "" {
		return `""`
	}
	lower := strings.ToLower(s)
	if lower == "true" || lower == "false" || lower == "null" || numberLiteralPattern.MatchString(s) {
		return strconv.Quote(s)
	}
	if safeStringPattern.MatchString(s) {
		return s
	}
	return strconv.Quote(s)
}
