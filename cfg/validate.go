package cfg

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidationError holds multiple schema validation failures.
type ValidationError struct {
	Errors []string
}

func (ve *ValidationError) Error() string {
	if len(ve.Errors) == 1 {
		return ve.Errors[0]
	}
	return "validation errors:\n  " + strings.Join(ve.Errors, "\n  ")
}

// SchemaRule defines constraints for a config key.
type SchemaRule struct {
	Type     string          // "string", "int", "bool", "float64", "section", "slice"
	Required bool            // key must exist
	Min      *int64          // numeric minimum (inclusive)
	Max      *int64          // numeric maximum (inclusive)
	Pattern  string          // regex for string values
	OneOf    []string        // allowed values
	Validate func(any) error // custom validation
}

// Schema is a set of rules keyed by config path.
type Schema map[string]SchemaRule

// Validate checks the config against a schema. Returns nil if valid.
func (c *Config) Validate(schema Schema) error {
	var errs []string
	for key, rule := range schema {
		val, exists := c.get(key)

		if rule.Required && !exists {
			errs = append(errs, fmt.Sprintf("key %q: required but missing", key))
			continue
		}
		if !exists {
			continue
		}

		// Type check
		if rule.Type != "" {
			if err := checkType(key, val, rule.Type); err != "" {
				errs = append(errs, err)
				continue
			}
		}

		// Numeric range
		if rule.Min != nil || rule.Max != nil {
			if n, err := asInt(val); err == nil {
				if rule.Min != nil && int64(n) < *rule.Min {
					errs = append(errs, fmt.Sprintf("key %q: value %d < min %d", key, n, *rule.Min))
				}
				if rule.Max != nil && int64(n) > *rule.Max {
					errs = append(errs, fmt.Sprintf("key %q: value %d > max %d", key, n, *rule.Max))
				}
			}
		}

		// Pattern
		if rule.Pattern != "" {
			s := asString(val)
			if matched, _ := regexp.MatchString(rule.Pattern, s); !matched {
				errs = append(errs, fmt.Sprintf("key %q: %q does not match pattern %q", key, s, rule.Pattern))
			}
		}

		// OneOf
		if len(rule.OneOf) > 0 {
			s := asString(val)
			found := false
			for _, opt := range rule.OneOf {
				if s == opt {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, fmt.Sprintf("key %q: %q not in %v", key, s, rule.OneOf))
			}
		}

		// Custom
		if rule.Validate != nil {
			if err := rule.Validate(val); err != nil {
				errs = append(errs, fmt.Sprintf("key %q: %v", key, err))
			}
		}
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

func checkType(key string, val any, expected string) string {
	ok := false
	switch expected {
	case "string":
		_, ok = val.(string)
	case "int":
		switch val.(type) {
		case int, int64:
			ok = true
		}
	case "bool":
		_, ok = val.(bool)
	case "float64":
		switch val.(type) {
		case float64, int:
			ok = true
		}
	case "section":
		_, ok = val.(map[string]any)
	case "slice":
		_, ok = val.([]any)
	default:
		return ""
	}
	if !ok {
		return fmt.Sprintf("key %q: expected type %s, got %T", key, expected, val)
	}
	return ""
}

// MinInt64 / MaxInt64 are helpers for creating *int64 for SchemaRule.
func MinInt64(n int64) *int64 { return &n }
func MaxInt64(n int64) *int64 { return &n }
