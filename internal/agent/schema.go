package agent

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// ValidateArgs checks tool arguments against the subset of JSON Schema the
// tools use: an object with typed properties, required keys, string length
// limits and enums. Unknown properties are ignored.
func ValidateArgs(schema map[string]any, args map[string]any) error {
	props, _ := schema["properties"].(map[string]any)
	var problems []string

	required, _ := schema["required"].([]string)
	for _, key := range required {
		v, ok := args[key]
		if !ok || v == nil {
			problems = append(problems, fmt.Sprintf("missing required argument %q", key))
		}
	}

	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v, ok := args[key]
		if !ok || v == nil {
			continue
		}
		spec, _ := props[key].(map[string]any)
		if err := validateValue(key, spec, v); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func validateValue(key string, spec map[string]any, v any) error {
	switch spec["type"] {
	case "string":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("argument %q must be a string", key)
		}
		n := utf8.RuneCountInString(strings.TrimSpace(s))
		if minLen, ok := spec["minLength"].(int); ok && n < minLen {
			return fmt.Errorf("argument %q must be at least %d characters", key, minLen)
		}
		if maxLen, ok := spec["maxLength"].(int); ok && n > maxLen {
			return fmt.Errorf("argument %q must be at most %d characters", key, maxLen)
		}
		if enum, ok := spec["enum"].([]string); ok && !slices.Contains(enum, s) {
			return fmt.Errorf("argument %q must be one of %v", key, enum)
		}
	case "integer":
		f, ok := v.(float64)
		if !ok || f != float64(int64(f)) {
			return fmt.Errorf("argument %q must be an integer", key)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("argument %q must be a boolean", key)
		}
	}
	return nil
}

// stringArg returns a trimmed string argument (validated beforehand).
func stringArg(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return strings.TrimSpace(s)
}
