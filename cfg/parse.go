package cfg

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Format selects the config file format.
type Format int

const (
	FormatAuto Format = iota
	FormatINI
	FormatSfc // Simple Fast Config: indent-based nesting, lists, typed scalars
	FormatJSON
)

// parse dispatches to the appropriate parser.
func (c *Config) parse(data []byte, format Format) error {
	if format == FormatAuto {
		format = detect(data)
	}
	var m map[string]any
	var err error
	switch format {
	case FormatJSON:
		m, err = parseJSON(data)
	case FormatSfc:
		m, err = parseSfc(data)
	case FormatINI:
		m, err = parseINI(data)
	default:
		return fmt.Errorf("cfg: unknown format %d", format)
	}
	if err != nil {
		return err
	}
	c.data.Store(m)
	return nil
}

// detect guesses format from content.
func detect(data []byte) Format {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return FormatINI
	}
	if trimmed[0] == '{' {
		return FormatJSON
	}
	// '[' could be JSON array or INI section header
	if trimmed[0] == '[' {
		// INI section: [word] possibly followed by newline
		end := bytes.IndexByte(trimmed, '\n')
		firstLine := trimmed
		if end > 0 {
			firstLine = trimmed[:end]
		}
		fl := strings.TrimSpace(string(firstLine))
		if len(fl) >= 2 && fl[len(fl)-1] == ']' && !strings.ContainsAny(fl, ",{}") {
			return FormatINI
		}
		return FormatJSON
	}
	// INI indicators: [section] or key=value on first non-comment line
	scanner := bufio.NewScanner(bytes.NewReader(trimmed))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			return FormatINI
		}
		if strings.Contains(line, "=") && !strings.Contains(line, ": ") && !strings.HasSuffix(line, ":") {
			return FormatINI
		}
		break
	}
	return FormatSfc
}

// --- JSON ---

func parseJSON(data []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("cfg: json parse error: %w", err)
	}
	// json.Unmarshal uses float64 for numbers; normalize to int where possible
	return normalizeJSON(m), nil
}

func normalizeJSON(m map[string]any) map[string]any {
	for k, v := range m {
		m[k] = normalizeJSONVal(v)
	}
	return m
}

func normalizeJSONVal(v any) any {
	switch val := v.(type) {
	case float64:
		if val == float64(int(val)) && val >= -1e15 && val <= 1e15 {
			return int(val)
		}
		return val
	case map[string]any:
		return normalizeJSON(val)
	case []any:
		for i, item := range val {
			val[i] = normalizeJSONVal(item)
		}
		return val
	default:
		return v
	}
}

// --- INI ---

func parseINI(data []byte) (map[string]any, error) {
	root := make(map[string]any)
	section := ""  // current section path (dotted)
	sliceKey := "" // current [[slice.key]] path (dotted)
	sliceItem := map[string]any(nil)

	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || trimmed[0] == '#' || trimmed[0] == ';' {
			continue
		}

		if len(trimmed) >= 4 && trimmed[0] == '[' && trimmed[1] == '[' && trimmed[len(trimmed)-2] == ']' && trimmed[len(trimmed)-1] == ']' {
			sliceKey = strings.TrimSpace(trimmed[2 : len(trimmed)-2])
			sliceItem = make(map[string]any)
			appendSliceItem(root, sliceKey, sliceItem)
			section = ""
			continue
		}

		if trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']' {
			section = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			sliceKey = ""
			sliceItem = nil
			continue
		}

		eqIdx := strings.IndexByte(trimmed, '=')
		if eqIdx < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eqIdx])
		val := strings.TrimSpace(trimmed[eqIdx+1:])

		val = unquoteINI(val)

		if sliceItem != nil {
			setNested(sliceItem, key, parseINIValue(val))
			continue
		}

		fullKey := key
		if section != "" {
			fullKey = section + "." + key
		}

		setNested(root, fullKey, parseINIValue(val))
	}
	return root, scanner.Err()
}

func appendSliceItem(root map[string]any, key string, item map[string]any) {
	parts := strings.Split(key, ".")
	cur := root
	for _, p := range parts[:len(parts)-1] {
		v, ok := cur[p]
		if !ok {
			next := make(map[string]any)
			cur[p] = next
			cur = next
		} else if next, ok := v.(map[string]any); ok {
			cur = next
		} else {
			next := make(map[string]any)
			cur[p] = next
			cur = next
		}
	}

	last := parts[len(parts)-1]
	v, ok := cur[last]
	if !ok {
		cur[last] = []any{item}
		return
	}
	if sl, ok := v.([]any); ok {
		cur[last] = append(sl, item)
		return
	}
	cur[last] = []any{item}
}

func unquoteINI(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func parseINIValue(s string) any {
	// Try bool
	lower := strings.ToLower(s)
	if lower == "true" || lower == "yes" || lower == "on" {
		return true
	}
	if lower == "false" || lower == "no" || lower == "off" {
		return false
	}
	// Try int
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	// Try float
	if f, err := strconv.ParseFloat(s, 64); err == nil && strings.Contains(s, ".") {
		return f
	}
	// Comma-separated list
	if strings.Contains(s, ",") && !strings.Contains(s, " = ") {
		parts := strings.Split(s, ",")
		list := make([]any, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				list = append(list, p)
			}
		}
		if len(list) > 1 {
			return list
		}
	}
	return s
}

// setNested sets a dotted key path in a nested map.
func setNested(root map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	cur := root
	for _, p := range parts[:len(parts)-1] {
		v, ok := cur[p]
		if !ok {
			next := make(map[string]any)
			cur[p] = next
			cur = next
		} else if next, ok := v.(map[string]any); ok {
			cur = next
		} else {
			next := make(map[string]any)
			cur[p] = next
			cur = next
		}
	}
	cur[parts[len(parts)-1]] = value
}

// --- SFC(yaml) ---
// Supports: maps, lists, scalars, multiline (| and >), flow sequences/mappings, comments.
// Does NOT support: anchors, aliases, tags, complex keys, multi-document.

const maxDepth = 64

func parseSfc(data []byte) (map[string]any, error) {
	lines := splitLines(data)
	result, _, err := parseSfcMap(lines, 0, -1, 0)
	if err != nil {
		return nil, fmt.Errorf("cfg: sfc parse error: %w", err)
	}
	if result == nil {
		return make(map[string]any), nil
	}
	return result, nil
}

type yline struct {
	indent int
	text   string // trimmed
	raw    string
}

func splitLines(data []byte) []yline {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var lines []yline
	for scanner.Scan() {
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		indent := countIndent(raw)
		lines = append(lines, yline{indent: indent, text: trimmed, raw: raw})
	}
	return lines
}

func countIndent(s string) int {
	n := 0
	for _, ch := range s {
		if ch == ' ' {
			n++
		} else if ch == '\t' {
			n += 2
		} else {
			break
		}
	}
	return n
}

func parseSfcMap(lines []yline, start, parentIndent, depth int) (map[string]any, int, error) {
	if depth > maxDepth {
		return nil, start, fmt.Errorf("max nesting depth exceeded")
	}
	result := make(map[string]any)
	i := start
	mapIndent := -1

	for i < len(lines) {
		line := lines[i]

		// Skip blank/comment
		if line.text == "" || line.text[0] == '#' {
			i++
			continue
		}

		// Determine map indent from first real line
		if mapIndent == -1 {
			mapIndent = line.indent
		}

		// If dedented, we're done with this map
		if line.indent < mapIndent {
			break
		}

		// Must be at map indent level
		if line.indent != mapIndent {
			// Could be continuation of previous value — skip
			i++
			continue
		}

		// List item at map level? Not a map entry.
		if strings.HasPrefix(line.text, "- ") || line.text == "-" {
			break
		}

		// Parse key: value
		colonIdx := strings.Index(line.text, ":")
		if colonIdx < 0 {
			i++
			continue
		}

		key := strings.TrimSpace(line.text[:colonIdx])
		rest := ""
		if colonIdx+1 < len(line.text) {
			rest = strings.TrimSpace(line.text[colonIdx+1:])
		}

		// Strip inline comment
		rest = stripComment(rest)

		if rest == "" {
			// Block value: could be nested map or list
			i++
			if i >= len(lines) {
				result[key] = nil
				continue
			}
			next := nextNonEmpty(lines, i)
			if next >= len(lines) {
				result[key] = nil
				continue
			}
			if strings.HasPrefix(lines[next].text, "- ") || lines[next].text == "-" {
				// List
				list, newI, err := parsSfcList(lines, next, mapIndent, depth+1)
				if err != nil {
					return nil, newI, err
				}
				result[key] = list
				i = newI
			} else if lines[next].indent > mapIndent {
				// Nested map
				sub, newI, err := parseSfcMap(lines, next, mapIndent, depth+1)
				if err != nil {
					return nil, newI, err
				}
				result[key] = sub
				i = newI
			} else {
				result[key] = nil
			}
		} else if rest == "|" || rest == ">" || rest == "|+" || rest == "|-" || rest == ">+" || rest == ">-" {
			// Multiline block scalar
			fold := rest[0] == '>'
			text, newI := readBlockScalar(lines, i+1, mapIndent)
			if fold {
				text = foldText(text)
			}
			result[key] = text
			i = newI
		} else if rest[0] == '[' {
			// Flow sequence
			result[key] = parseFlowSeq(rest)
			i++
		} else if rest[0] == '{' {
			// Flow mapping
			result[key] = parseFlowMap(rest)
			i++
		} else {
			// Scalar
			result[key] = parseScalar(rest)
			i++
		}
	}
	return result, i, nil
}

func parsSfcList(lines []yline, start, parentIndent, depth int) ([]any, int, error) {
	if depth > maxDepth {
		return nil, start, fmt.Errorf("max nesting depth exceeded")
	}
	var result []any
	i := start
	listIndent := -1

	for i < len(lines) {
		line := lines[i]

		if line.text == "" || line.text[0] == '#' {
			i++
			continue
		}

		if listIndent == -1 {
			listIndent = line.indent
		}

		if line.indent < listIndent {
			break
		}
		if line.indent != listIndent {
			i++
			continue
		}

		if !strings.HasPrefix(line.text, "- ") && line.text != "-" {
			break
		}

		// Get item content after "- "
		itemText := ""
		if len(line.text) > 2 {
			itemText = strings.TrimSpace(line.text[2:])
		}

		if itemText == "" || itemText == "-" {
			// Empty item or nested
			i++
			next := nextNonEmpty(lines, i)
			if next < len(lines) && lines[next].indent > listIndent {
				if strings.HasPrefix(lines[next].text, "- ") {
					sub, newI, err := parsSfcList(lines, next, listIndent, depth+1)
					if err != nil {
						return nil, newI, err
					}
					result = append(result, sub)
					i = newI
				} else {
					sub, newI, err := parseSfcMap(lines, next, listIndent, depth+1)
					if err != nil {
						return nil, newI, err
					}
					result = append(result, sub)
					i = newI
				}
			} else {
				result = append(result, nil)
			}
		} else if strings.Contains(itemText, ": ") || strings.HasSuffix(itemText, ":") {
			// Inline map item: - key: value (with possible continuation keys)
			colonIdx := strings.Index(itemText, ":")
			k := strings.TrimSpace(itemText[:colonIdx])
			v := ""
			if colonIdx+1 < len(itemText) {
				v = strings.TrimSpace(itemText[colonIdx+1:])
			}
			v = stripComment(v)
			item := make(map[string]any)
			newI := i + 1
			if v == "" {
				next := nextNonEmpty(lines, i+1)
				if next < len(lines) && lines[next].indent > listIndent+2 {
					if strings.HasPrefix(lines[next].text, "- ") {
						nested, ni, e := parsSfcList(lines, next, listIndent+2, depth+1)
						if e != nil {
							return nil, ni, e
						}
						item[k] = nested
						newI = ni
					} else {
						nested, ni, e := parseSfcMap(lines, next, listIndent+2, depth+1)
						if e != nil {
							return nil, ni, e
						}
						item[k] = nested
						newI = ni
					}
				} else {
					item[k] = nil
				}
			} else {
				item[k] = parseValue(v)
			}
			// Continuation keys at listIndent+2
			for newI < len(lines) {
				cl := lines[newI]
				if cl.text == "" || cl.text[0] == '#' {
					newI++
					continue
				}
				if cl.indent != listIndent+2 {
					break
				}
				cIdx := strings.Index(cl.text, ":")
				if cIdx < 0 {
					break
				}
				ck := strings.TrimSpace(cl.text[:cIdx])
				cv := ""
				if cIdx+1 < len(cl.text) {
					cv = strings.TrimSpace(cl.text[cIdx+1:])
				}
				cv = stripComment(cv)
				if cv == "" {
					next := nextNonEmpty(lines, newI+1)
					if next < len(lines) && lines[next].indent > listIndent+2 {
						if strings.HasPrefix(lines[next].text, "- ") {
							nested, ni, e := parsSfcList(lines, next, listIndent+2, depth+1)
							if e != nil {
								return nil, ni, e
							}
							item[ck] = nested
							newI = ni
						} else {
							nested, ni, e := parseSfcMap(lines, next, listIndent+2, depth+1)
							if e != nil {
								return nil, ni, e
							}
							item[ck] = nested
							newI = ni
						}
					} else {
						item[ck] = nil
						newI++
					}
				} else {
					item[ck] = parseValue(cv)
					newI++
				}
			}
			result = append(result, item)
			i = newI
		} else {
			// Simple scalar item
			result = append(result, parseValue(itemText))
			i++
		}
	}
	return result, i, nil
}

func readBlockScalar(lines []yline, start, baseIndent int) (string, int) {
	i := start
	blockIndent := -1
	var parts []string

	for i < len(lines) {
		line := lines[i]
		if line.text == "" {
			parts = append(parts, "")
			i++
			continue
		}
		if blockIndent == -1 {
			blockIndent = line.indent
		}
		if line.indent < blockIndent {
			break
		}
		// Strip the block indent prefix
		raw := line.raw
		stripped := ""
		if len(raw) > blockIndent {
			stripped = raw[blockIndent:]
		}
		parts = append(parts, stripped)
		i++
	}

	// Trim trailing empty lines
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "\n"), i
}

func foldText(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	var current string
	for _, line := range lines {
		if line == "" {
			if current != "" {
				result = append(result, current)
				current = ""
			}
			result = append(result, "")
		} else {
			if current != "" {
				current += " " + line
			} else {
				current = line
			}
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return strings.Join(result, "\n")
}

func parseScalar(s string) any {
	s = stripComment(s)
	if s == "" || s == "null" || s == "~" {
		return nil
	}
	if s == "true" || s == "yes" || s == "on" {
		return true
	}
	if s == "false" || s == "no" || s == "off" {
		return false
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return interpretDoubleQuoteEscapes(s[1 : len(s)-1])
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	// Int
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	// Float
	if f, err := strconv.ParseFloat(s, 64); err == nil && strings.ContainsAny(s, ".eE") {
		return f
	}
	return s
}

func parseValue(s string) any {
	s = strings.TrimSpace(s)
	if len(s) > 0 && s[0] == '[' {
		return parseFlowSeq(s)
	}
	if len(s) > 0 && s[0] == '{' {
		return parseFlowMap(s)
	}
	return parseScalar(s)
}

func parseFlowSeq(s string) []any {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil
	}
	inner := s[1 : len(s)-1]
	if strings.TrimSpace(inner) == "" {
		return []any{}
	}
	parts := splitFlow(inner)
	result := make([]any, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, parseScalar(p))
		}
	}
	return result
}

func parseFlowMap(s string) map[string]any {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil
	}
	inner := s[1 : len(s)-1]
	if strings.TrimSpace(inner) == "" {
		return map[string]any{}
	}
	parts := splitFlow(inner)
	result := make(map[string]any, len(parts))
	for _, p := range parts {
		colonIdx := strings.Index(p, ":")
		if colonIdx < 0 {
			continue
		}
		k := strings.TrimSpace(p[:colonIdx])
		v := strings.TrimSpace(p[colonIdx+1:])
		result[k] = parseScalar(v)
	}
	return result
}

func splitFlow(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, ch := range s {
		switch ch {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}

func interpretDoubleQuoteEscapes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i += 2
			case 'r':
				b.WriteByte('\r')
				i += 2
			case 't':
				b.WriteByte('\t')
				i += 2
			case '\\':
				b.WriteByte('\\')
				i += 2
			case '"':
				b.WriteByte('"')
				i += 2
			case '0':
				b.WriteByte(0)
				i += 2
			case 'a':
				b.WriteByte('\a')
				i += 2
			case 'b':
				b.WriteByte('\b')
				i += 2
			case 'f':
				b.WriteByte('\f')
				i += 2
			case 'v':
				b.WriteByte('\v')
				i += 2
			default:
				b.WriteByte(s[i])
				i++
			}
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

func stripComment(s string) string {
	// Only strip if # is preceded by space (not inside string)
	if idx := strings.Index(s, " #"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	return s
}

func nextNonEmpty(lines []yline, start int) int {
	for i := start; i < len(lines); i++ {
		if lines[i].text != "" && lines[i].text[0] != '#' {
			return i
		}
	}
	return len(lines)
}
