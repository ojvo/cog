// Package util - clap: Declarative CLI argument parsing via struct tags.
//
// Parses command-line arguments into a struct. Tag format:
//
//	`clap:"longname[,shortname][,options...]" description:"text" env:"VAR"`
//
// Options: mandatory, greedy, once
//
// Supported field types:
//
//	int, int8-64, uint, uint8-64, float32/64, bool, string, time.Duration,
//	[]int, []int64, []float64, []string, [N]T arrays,
//	pointer variants of the above, any type implementing encoding.TextUnmarshaler.
//
// Special longname "trailing" collects all remaining positional args into a []string.

// Typical usage:
//
//	type cfg struct {
//	    Name   string        `clap:"name,n,mandatory" description:"your name"`
//	    Count  int           `clap:"count,c" description:"repeat count" env:"COUNT"`
//	    Debug  bool          `clap:"debug,d"`
//	    Files  []string      `clap:"files,f,greedy" description:"input files"`
//	    Rest   []string      `clap:"trailing"`
//	}
//	result, err := clap.Parse(os.Args[1:], &cfg)
//	if result.HasErrors() { ... }
//	fmt.Println(clap.Usage(cfg))
package util

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	ErrUnexpectedArgument   = errors.New("unexpected argument")
	ErrMissingArgumentValue = errors.New("missing argument value")
	ErrIgnoredArgument      = errors.New("ignored argument")
	ErrMandatoryArgument    = errors.New("mandatory argument")
	ErrDuplicatedArgument   = errors.New("duplicated argument")
	ErrInvalidTag           = errors.New("invalid tag")
)

// ---------------------------------------------------------------------------
// Results
// ---------------------------------------------------------------------------

// ClapResults reports parse issues.
type ClapResults struct {
	Unexpected []string // type mismatch
	Missing    []string // missing value for non-bool flag
	Ignored    []string // unrecognized flags (warnings)
	Mandatory  []string // mandatory flags not found
	Duplicated []string // duplicate flags
}

// HasErrors returns true if any error-level result is present.
// (Unexpected, Missing, Mandatory, Duplicated are errors;
// Ignored is a warning.)
func (r *ClapResults) HasErrors() bool {
	return len(r.Unexpected) != 0 || len(r.Missing) != 0 ||
		len(r.Mandatory) != 0 || len(r.Duplicated) != 0
}

// HasWarnings returns true if any warning-level result is present.
func (r *ClapResults) HasWarnings() bool { return len(r.Ignored) != 0 }

// ---------------------------------------------------------------------------
// fieldDescription
// ---------------------------------------------------------------------------

type fieldDescription struct {
	Field       int
	ShortName   string
	LongName    string
	Type        reflect.Type
	Args        []string
	Mandatory   bool
	Greedy      bool
	Once        bool
	EnvName     string
	Found       bool
	Visited     bool
	Description string
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// ClapParse parses command-line arguments into the struct pointed to by cfg.
// Fields are matched by `clap` struct tag; untagged fields are ignored.
func ClapParse[T any](args []string, cfg *T) (*ClapResults, error) {
	var err error
	var results *ClapResults
	var fieldDescs map[string]*fieldDescription
	fieldDescs, err = computeFieldDescriptions(reflect.TypeOf(*cfg))
	if err != nil {
		return nil, err
	}
	if results, err = fillStruct(args, fieldDescs, cfg); err != nil {
		return results, err
	}
	return results, nil
}

// ClapParseArg is a shortcut for ClapParse(os.Args[1:], cfg).
func ClapParseArg[T any](cfg *T) (*ClapResults, error) {
	return ClapParse(os.Args[1:], cfg)
}

// ---------------------------------------------------------------------------
// Usage
// ---------------------------------------------------------------------------

// Usage generates a help string from the struct definition.
// Accepts either a value or pointer to the config struct.
func Usage(cfg any) string {
	v := reflect.ValueOf(cfg)
	t := v.Type()
	if t.Kind() == reflect.Ptr {
		if v.IsNil() {
			t = t.Elem()
		} else {
			v = v.Elem()
			t = v.Type()
		}
	}

	fieldDescs, err := computeFieldDescriptions(t)
	if err != nil {
		return fmt.Sprintf("Error generating usage: %v", err)
	}

	uniqueDescs := make(map[int]*fieldDescription)
	for _, desc := range fieldDescs {
		uniqueDescs[desc.Field] = desc
	}

	var sortedDescs []*fieldDescription
	for _, desc := range uniqueDescs {
		sortedDescs = append(sortedDescs, desc)
	}
	sort.Slice(sortedDescs, func(i, j int) bool {
		return sortedDescs[i].Field < sortedDescs[j].Field
	})

	var sb strings.Builder
	sb.WriteString("Usage:\n")

	for _, desc := range sortedDescs {
		if desc.Type == nil {
			continue
		}

		var flags []string
		if desc.ShortName != "" {
			flags = append(flags, "-"+desc.ShortName)
		}
		if desc.LongName != "" {
			flags = append(flags, "--"+desc.LongName)
		}

		isTrailing := false
		for k, d := range fieldDescs {
			if d == desc && k == trailing {
				isTrailing = true
				break
			}
		}

		if isTrailing {
			sb.WriteString(fmt.Sprintf("  [...%s]\n", desc.Type.Elem().Kind()))
			if desc.Description != "" {
				sb.WriteString(fmt.Sprintf("      %s\n", desc.Description))
			}
			continue
		}

		if len(flags) > 0 {
			sb.WriteString(fmt.Sprintf("  %s", strings.Join(flags, ", ")))

			if desc.Type.Kind() != reflect.Bool {
				typeName := desc.Type.String()
				if typeName == "time.Duration" {
					typeName = "duration"
				}
				sb.WriteString(fmt.Sprintf(" <%s>", typeName))
			}

			if desc.Mandatory {
				sb.WriteString(" (mandatory)")
			}

			if desc.Greedy {
				sb.WriteString(" (greedy)")
			}

			if desc.EnvName != "" {
				sb.WriteString(fmt.Sprintf(" [env: %s]", desc.EnvName))
			}

			if !desc.Mandatory && v.IsValid() && v.Kind() == reflect.Struct {
				f := v.Field(desc.Field)
				if !f.IsZero() {
					val := f
					if f.Kind() == reflect.Ptr {
						val = f.Elem()
					}
					sb.WriteString(fmt.Sprintf(" (default: %v)", val))
				}
			}

			sb.WriteString("\n")
			if desc.Description != "" {
				sb.WriteString(fmt.Sprintf("      %s\n", desc.Description))
			}
		}
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// Tag parsing
// ---------------------------------------------------------------------------

const (
	trailing  string = "trailing"
	mandatory string = "mandatory"
	optGreedy string = "greedy"
	optOnce   string = "once"
)

func getTrailingFieldDescription(tags []string, field reflect.StructField) (*fieldDescription, error) {
	fd := &fieldDescription{Type: field.Type}
	if len(tags) != 1 {
		return nil, fmt.Errorf("field '%s': %w (got '%s', expected 'trailing')",
			field.Name, ErrInvalidTag, field.Tag.Get("clap"))
	}
	if fd.Type.Kind() != reflect.Slice || fd.Type.Elem().Kind() != reflect.String {
		return nil, fmt.Errorf("field '%s' should be a []string: %w", field.Name, ErrInvalidTag)
	}
	return fd, nil
}

func getShortNameFieldDescription(tags []string, field reflect.StructField) (*fieldDescription, error) {
	fd := &fieldDescription{Type: field.Type}
	if len(tags) > 3 || len(tags) < 2 {
		return nil, fmt.Errorf("field '%s': %w (got '%s', expected two or three values)",
			field.Name, ErrInvalidTag, field.Tag.Get("clap"))
	}
	fd.ShortName = strings.Trim(tags[1], " -")
	if len(fd.ShortName) != 1 {
		return nil, fmt.Errorf("field '%s': %w (got '%s', expected a single char value)",
			field.Name, ErrInvalidTag, field.Tag.Get("clap"))
	}
	if len(tags) == 3 {
		if err := parseFieldOption(fd, tags[2]); err != nil {
			return nil, fmt.Errorf("field '%s': %w", field.Name, err)
		}
	}
	return fd, nil
}

func getLongNameFieldDescription(tags []string, field reflect.StructField) (*fieldDescription, error) {
	fd := &fieldDescription{Type: field.Type}
	if len(tags) > 1 {
		for i := 1; i < len(tags); i++ {
			tag := strings.Trim(tags[i], " -")
			switch {
			case len(tag) == 1 && tag != mandatory && tag != optGreedy && tag != optOnce:
				if fd.ShortName != "" {
					return nil, fmt.Errorf("field '%s': %w (multiple short names)",
						field.Name, ErrInvalidTag)
				}
				fd.ShortName = tag
			case tag == mandatory || tag == optGreedy || tag == optOnce:
				if err := parseFieldOption(fd, tag); err != nil {
					return nil, fmt.Errorf("field '%s': %w", field.Name, err)
				}
			default:
				// ignore unknown tags for forwards compatibility
			}
		}
	}
	return fd, nil
}

func parseFieldOption(fd *fieldDescription, opt string) error {
	switch opt {
	case mandatory:
		fd.Mandatory = true
	case optGreedy:
		fd.Greedy = true
	case optOnce:
		fd.Once = true
	default:
		return fmt.Errorf("unknown option '%s': %w", opt, ErrInvalidTag)
	}
	return nil
}

// isShortOnlyTag reports whether the first tag element represents a
// short-name-only flag like "-O" (as opposed to a long name like "--verbose").
func isShortOnlyTag(tag string) bool {
	return len(tag) == 2 && tag[0] == '-' && tag != trailing
}

func computeFieldDescriptions(t reflect.Type) (map[string]*fieldDescription, error) {
	fieldDescs := make(map[string]*fieldDescription)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tagString := field.Tag.Get("clap")
		if tagString != "" {
			var fieldDesc *fieldDescription
			tags := strings.Split(tagString, ",")
			tag := strings.Trim(tags[0], " ")
			switch {
			case tag == trailing:
				var err error
				fieldDesc, err = getTrailingFieldDescription(tags, field)
				if err != nil {
					return nil, err
				}
				fieldDescs[trailing] = fieldDesc
			case tag == "":
				// Starts with ",": short name only, e.g., ",-s" or ",-s,mandatory"
				var err error
				fieldDesc, err = getShortNameFieldDescription(tags, field)
				if err != nil {
					return nil, err
				}
				fieldDescs["-"+fieldDesc.ShortName] = fieldDesc
			case isShortOnlyTag(tag):
				// "-O" pattern: short name only; rewrite tags to match
				// the ",-s" convention expected by getShortNameFieldDescription.
				rewritten := make([]string, len(tags)+1)
				rewritten[0] = ""
				rewritten[1] = tag[1:] // strip leading "-"
				copy(rewritten[2:], tags[1:])
				var err error
				fieldDesc, err = getShortNameFieldDescription(rewritten, field)
				if err != nil {
					return nil, err
				}
				fieldDescs["-"+fieldDesc.ShortName] = fieldDesc
			default:
				var err error
				fieldDesc, err = getLongNameFieldDescription(tags, field)
				if err != nil {
					return nil, err
				}
				fieldDesc.LongName = strings.Trim(tag, "-")
				if fieldDesc.LongName != "" {
					fieldDescs["--"+fieldDesc.LongName] = fieldDesc
					if field.Type.Kind() == reflect.Bool {
						fieldDescs["--no-"+fieldDesc.LongName] = fieldDesc
					}
				}
				if fieldDesc.ShortName != "" {
					fieldDescs["-"+fieldDesc.ShortName] = fieldDesc
				}
			}
			fieldDesc.Field = i
			fieldDesc.Description = field.Tag.Get("description")
			// Environment variable fallback
			if envName := field.Tag.Get("env"); envName != "" {
				fieldDesc.EnvName = envName
			}
		}
	}
	return fieldDescs, nil
}

// ---------------------------------------------------------------------------
// Argument consumption and conversion helpers
// ---------------------------------------------------------------------------

func consumeArguments(start int, args []string, count int) (int, []string) {
	var values []string
	for ; start < count; start++ {
		if strings.HasPrefix(args[start], "-") {
			break
		}
		values = append(values, args[start])
	}
	return start - 1, values
}

func stringsToInts(strs []string) ([]int, error) {
	var ints []int
	for _, str := range strs {
		val, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w (got '%s', expected integer)", err, str)
		}
		ints = append(ints, int(val))
	}
	return ints, nil
}

func stringsToInt64s(strs []string) ([]int64, error) {
	var ints []int64
	for _, str := range strs {
		val, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w (got '%s', expected integer)", err, str)
		}
		ints = append(ints, val)
	}
	return ints, nil
}

func stringsToFloat64s(strs []string) ([]float64, error) {
	var floats []float64
	for _, str := range strs {
		val, err := strconv.ParseFloat(str, 64)
		if err != nil {
			return nil, fmt.Errorf("%w (got '%s', expected float)", err, str)
		}
		floats = append(floats, val)
	}
	return floats, nil
}

// getEffectiveType returns the underlying type after stripping pointers.
func getEffectiveType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// ---------------------------------------------------------------------------
// Core parsing
// ---------------------------------------------------------------------------

func argsToFields(args []string, fieldDescs map[string]*fieldDescription, cfg any) (*ClapResults, error) {
	results := &ClapResults{}
	reflectValue := reflect.ValueOf(cfg).Elem()
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if desc, ok := fieldDescs[arg]; ok {
			if desc.Found && desc.Once {
				results.Duplicated = append(results.Duplicated, arg)
				return results, fmt.Errorf("argument '%s': %w", arg, ErrDuplicatedArgument)
			}
			if desc.Found && !desc.Greedy {
				results.Duplicated = append(results.Duplicated, arg)
				return results, fmt.Errorf("argument '%s': %w", arg, ErrDuplicatedArgument)
			}
			desc.Found = true
			field := reflectValue.Field(desc.Field)
			if !field.CanSet() {
				continue
			}

			effType := getEffectiveType(desc.Type)

			isTextUnmarshaler := reflect.PtrTo(effType).Implements(reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem())

			if isTextUnmarshaler {
				i++
				if i >= len(args) || strings.HasPrefix(args[i], "-") {
					results.Missing = append(results.Missing, arg)
					return results, fmt.Errorf("argument '%s': %w", arg, ErrMissingArgumentValue)
				}
				desc.Args = append(desc.Args, args[i])
				continue
			}

			switch effType.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
				reflect.String, reflect.Float32, reflect.Float64:
				i++
				if i >= len(args) || strings.HasPrefix(args[i], "-") {
					results.Missing = append(results.Missing, arg)
					return results, fmt.Errorf("argument '%s': %w", arg, ErrMissingArgumentValue)
				}
				desc.Args = append(desc.Args, args[i])
			case reflect.Bool:
				desc.Args = append(desc.Args, fmt.Sprintf("%v", !strings.HasPrefix(arg, "--no-")))
			case reflect.Slice, reflect.Array:
				var values []string
				count := len(args)
				if effType.Kind() == reflect.Array {
					count = i + 1 + effType.Len()
				}
				i, values = consumeArguments(i+1, args, count)
				if len(values) == 0 {
					results.Missing = append(results.Missing, arg)
					return results, fmt.Errorf("argument '%s': %w", arg, ErrMissingArgumentValue)
				}
				desc.Args = append(desc.Args, values...)
			}
		} else {
			found := false
			for j := i; j < len(args); j++ {
				if strings.HasPrefix(args[j], "-") {
					results.Ignored = append(results.Ignored, arg)
					found = true
					break
				}
			}
			if !found {
				if desc, ok := fieldDescs[trailing]; ok {
					field := reflectValue.Field(desc.Field)
					if field.CanSet() {
						var values []string
						for j := i; j < len(args); j++ {
							values = append(values, args[j])
						}
						desc.Args = append(desc.Args, values...)
						break
					}
				}
			}
		}
	}

	// Check mandatory
	for _, desc := range fieldDescs {
		if !desc.Found && desc.Mandatory {
			name := desc.LongName
			if name == "" {
				name = desc.ShortName
			}
			results.Mandatory = append(results.Mandatory, name)
		}
	}
	if len(results.Mandatory) != 0 {
		return results, fmt.Errorf("mandatory argument/s: '%v' not found: %w",
			strings.Join(results.Mandatory, ","), ErrMandatoryArgument)
	}

	return results, nil
}

func fillStruct(args []string, fieldDescs map[string]*fieldDescription, cfg any) (*ClapResults, error) {
	// Apply environment variable defaults before parsing
	applyEnvDefaults(fieldDescs, cfg)

	results, err := argsToFields(args, fieldDescs, cfg)
	if err != nil {
		return results, err
	}
	reflectValue := reflect.ValueOf(cfg).Elem()
	for name, desc := range fieldDescs {
		field := reflectValue.Field(desc.Field)
		if !field.CanSet() || len(desc.Args) == 0 || desc.Visited {
			continue
		}
		desc.Visited = true
		if err := setField(field, desc.Args, name, results); err != nil {
			return results, err
		}
	}
	return results, nil
}

func applyEnvDefaults(fieldDescs map[string]*fieldDescription, cfg any) {
	reflectValue := reflect.ValueOf(cfg).Elem()
	for _, desc := range fieldDescs {
		if desc.EnvName == "" {
			continue
		}
		if v, ok := os.LookupEnv(desc.EnvName); ok {
			field := reflectValue.Field(desc.Field)
			if field.CanSet() && desc.Type.Kind() == reflect.Bool {
				// For booleans, set true unless env is explicitly "false"
				if strings.ToLower(v) != "false" {
					_ = setField(field, []string{"true"}, desc.EnvName, &ClapResults{})
				}
			} else if field.CanSet() && (desc.Type.Kind() == reflect.Slice || desc.Type.Kind() == reflect.Array) {
				// Split by comma for slices
				parts := strings.Split(v, ",")
				trimmed := make([]string, 0, len(parts))
				for _, p := range parts {
					if s := strings.TrimSpace(p); s != "" {
						trimmed = append(trimmed, s)
					}
				}
				_ = setField(field, trimmed, desc.EnvName, &ClapResults{})
				desc.Args = trimmed // mark as having default value
			} else if field.CanSet() {
				_ = setField(field, []string{v}, desc.EnvName, &ClapResults{})
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Field value setting
// ---------------------------------------------------------------------------

func setField(field reflect.Value, args []string, name string, results *ClapResults) error {
	// Handle pointers recursively
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return setField(field.Elem(), args, name, results)
	}

	// Check for TextUnmarshaler on the addressable value
	if field.CanAddr() {
		addr := field.Addr()
		if unmarshaler, ok := addr.Interface().(encoding.TextUnmarshaler); ok {
			if len(args) > 0 {
				if err := unmarshaler.UnmarshalText([]byte(args[0])); err != nil {
					results.Unexpected = append(results.Unexpected, name)
					return fmt.Errorf("argument '%s': %w", name, err)
				}
				return nil
			}
		}
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(args[0])
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			val, err := time.ParseDuration(args[0])
			if err != nil {
				results.Unexpected = append(results.Unexpected, name)
				return fmt.Errorf("argument '%s': %w (got '%s', expected duration)",
					name, ErrUnexpectedArgument, args[0])
			}
			field.SetInt(int64(val))
		} else {
			val, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				results.Unexpected = append(results.Unexpected, name)
				return fmt.Errorf("argument '%s': %w (got '%s', expected integer)",
					name, ErrUnexpectedArgument, args[0])
			}
			field.SetInt(val)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			results.Unexpected = append(results.Unexpected, name)
			return fmt.Errorf("argument '%s': %w (got '%s', expected integer)",
				name, ErrUnexpectedArgument, args[0])
		}
		field.SetUint(val)
	case reflect.Float32, reflect.Float64:
		val, err := strconv.ParseFloat(args[0], 64)
		if err != nil {
			results.Unexpected = append(results.Unexpected, name)
			return fmt.Errorf("argument '%s': %w (got '%s', expected float)",
				name, ErrUnexpectedArgument, args[0])
		}
		field.SetFloat(val)
	case reflect.Bool:
		field.SetBool(args[0] == "true")
	case reflect.Map:
		if len(args) > 0 {
			// Try JSON unmarshal for map fields
			if err := json.Unmarshal([]byte(args[0]), field.Addr().Interface()); err != nil {
				results.Unexpected = append(results.Unexpected, name)
				return fmt.Errorf("argument '%s': %w (got '%s', expected JSON map)",
					name, ErrUnexpectedArgument, args[0])
			}
		}
	case reflect.Slice:
		switch {
		case field.Type().Elem().Kind() == reflect.String:
			field.Set(reflect.ValueOf(args))
		case field.Type().Elem().Kind() == reflect.Int:
			ints, err := stringsToInts(args)
			if err != nil {
				results.Unexpected = append(results.Unexpected, name)
				return fmt.Errorf("argument '%s': %w", name, err)
			}
			field.Set(reflect.ValueOf(ints))
		case field.Type().Elem().Kind() == reflect.Int64:
			ints, err := stringsToInt64s(args)
			if err != nil {
				results.Unexpected = append(results.Unexpected, name)
				return fmt.Errorf("argument '%s': %w", name, err)
			}
			field.Set(reflect.ValueOf(ints))
		case field.Type().Elem().Kind() == reflect.Float64:
			floats, err := stringsToFloat64s(args)
			if err != nil {
				results.Unexpected = append(results.Unexpected, name)
				return fmt.Errorf("argument '%s': %w", name, err)
			}
			field.Set(reflect.ValueOf(floats))
		}
	case reflect.Array:
		switch {
		case field.Type().Elem().Kind() == reflect.String:
			arrayType := reflect.ArrayOf(field.Type().Len(), field.Type().Elem())
			v := reflect.New(arrayType).Elem()
			reflect.Copy(v, reflect.ValueOf(args))
			field.Set(v)
		case field.Type().Elem().Kind() == reflect.Int:
			ints, err := stringsToInts(args)
			if err != nil {
				results.Unexpected = append(results.Unexpected, name)
				return fmt.Errorf("argument '%s': %w", name, err)
			}
			arrayType := reflect.ArrayOf(field.Type().Len(), field.Type().Elem())
			v := reflect.New(arrayType).Elem()
			reflect.Copy(v, reflect.ValueOf(ints))
			field.Set(v)
		case field.Type().Elem().Kind() == reflect.Int64:
			ints, err := stringsToInt64s(args)
			if err != nil {
				results.Unexpected = append(results.Unexpected, name)
				return fmt.Errorf("argument '%s': %w", name, err)
			}
			arrayType := reflect.ArrayOf(field.Type().Len(), field.Type().Elem())
			v := reflect.New(arrayType).Elem()
			reflect.Copy(v, reflect.ValueOf(ints))
			field.Set(v)
		case field.Type().Elem().Kind() == reflect.Float64:
			floats, err := stringsToFloat64s(args)
			if err != nil {
				results.Unexpected = append(results.Unexpected, name)
				return fmt.Errorf("argument '%s': %w", name, err)
			}
			arrayType := reflect.ArrayOf(field.Type().Len(), field.Type().Elem())
			v := reflect.New(arrayType).Elem()
			reflect.Copy(v, reflect.ValueOf(floats))
			field.Set(v)
		}
	}
	return nil
}
