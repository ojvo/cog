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
	if trimmed[0] == '[' {
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
	data = trimUTF8BOM(data)
	root := make(map[string]any)
	section := ""
	sliceItem := map[string]any(nil)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var continuation strings.Builder
	var pendingKey string
	var pendingQuote string
	var pendingVal strings.Builder

	flushMultiline := func() {
		val := strings.TrimSuffix(pendingVal.String(), "\n")
		if sliceItem != nil {
			setNested(sliceItem, pendingKey, val)
		} else {
			fullKey := pendingKey
			if section != "" {
				fullKey = section + "." + pendingKey
			}
			setNested(root, fullKey, val)
		}
		pendingKey = ""
		pendingQuote = ""
		pendingVal.Reset()
	}

	for scanner.Scan() {
		line := scanner.Text()

		if pendingQuote != "" {
			clean := line
			trimmed := strings.TrimSpace(clean)
			if strings.HasSuffix(trimmed, pendingQuote) {
				end := strings.LastIndex(clean, pendingQuote)
				chunk := clean[:end]
				if chunk != "" {
					if pendingVal.Len() > 0 {
						pendingVal.WriteByte('\n')
					}
					pendingVal.WriteString(chunk)
				}
				flushMultiline()
				continue
			}
			if pendingVal.Len() > 0 {
				pendingVal.WriteByte('\n')
			}
			pendingVal.WriteString(clean)
			continue
		}

		if strings.HasSuffix(line, `\`) {
			continuation.WriteString(strings.TrimSuffix(line, `\`))
			continue
		}
		if continuation.Len() > 0 {
			continuation.WriteString(line)
			line = continuation.String()
			continuation.Reset()
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isINIComment(trimmed) {
			continue
		}

		if len(trimmed) >= 4 && strings.HasPrefix(trimmed, "[[") && strings.HasSuffix(trimmed, "]]") {
			sliceKey := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
			sliceItem = make(map[string]any)
			appendSliceItem(root, sliceKey, sliceItem)
			section = ""
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			sliceItem = nil
			continue
		}

		idx := firstINISeparator(trimmed)
		if idx < 0 {
			return nil, fmt.Errorf("cfg: ini parse error: missing key/value separator in %q", trimmed)
		}
		key := strings.TrimSpace(trimmed[:idx])
		val := strings.TrimSpace(trimmed[idx+1:])
		if key == "" {
			return nil, fmt.Errorf("cfg: ini parse error: empty key")
		}

		if strings.HasPrefix(val, `"""`) || strings.HasPrefix(val, `'''`) {
			pendingKey = key
			pendingQuote = val[:3]
			rest := strings.TrimPrefix(val, pendingQuote)
			if strings.HasSuffix(rest, pendingQuote) {
				rest = strings.TrimSuffix(rest, pendingQuote)
				if sliceItem != nil {
					setNested(sliceItem, pendingKey, rest)
				} else {
					fullKey := pendingKey
					if section != "" {
						fullKey = section + "." + pendingKey
					}
					setNested(root, fullKey, rest)
				}
				pendingKey = ""
				pendingQuote = ""
				continue
			}
			pendingVal.WriteString(rest)
			continue
		}

		val = stripINIInlineComment(val)
		val = unquoteINI(val)
		parsed := parseINIValue(val)
		if sliceItem != nil {
			setNested(sliceItem, key, parsed)
			continue
		}
		fullKey := key
		if section != "" {
			fullKey = section + "." + key
		}
		setNested(root, fullKey, parsed)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if pendingQuote != "" {
		return nil, fmt.Errorf("cfg: ini parse error: unclosed multiline value for key %s", pendingKey)
	}
	if continuation.Len() > 0 {
		return nil, fmt.Errorf("cfg: ini parse error: unclosed continuation line")
	}
	return root, nil
}

func trimUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func isINIComment(s string) bool {
	return strings.HasPrefix(s, "#") || strings.HasPrefix(s, ";")
}

func firstINISeparator(s string) int {
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuote != 0 {
			if ch == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if ch == inQuote {
				inQuote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			inQuote = ch
			continue
		}
		if ch == '=' || ch == ':' {
			return i
		}
	}
	return -1
}

func stripINIInlineComment(s string) string {
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuote != 0 {
			if ch == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if ch == inQuote {
				inQuote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			inQuote = ch
			continue
		}
		if ch == '#' || ch == ';' {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
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
	lower := strings.ToLower(s)
	if lower == "true" || lower == "yes" || lower == "on" || lower == "enabled" {
		return true
	}
	if lower == "false" || lower == "no" || lower == "off" || lower == "disabled" {
		return false
	}
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && strings.Contains(s, ".") {
		return f
	}
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
	text   string
	raw    string
}

func splitLines(data []byte) []yline {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var lines []yline
	for scanner.Scan() {
		raw := scanner.Text()
		lines = append(lines, yline{indent: countIndent(raw), text: strings.TrimSpace(raw), raw: raw})
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

func parseSfcMap(lines []yline, start int, parentIndent int, depth int) (map[string]any, int, error) {
	if depth > maxDepth {
		return nil, start, fmt.Errorf("max depth exceeded")
	}
	result := make(map[string]any)
	i := start
	for i < len(lines) {
		line := lines[i]
		if line.text == "" || strings.HasPrefix(line.text, "#") {
			i++
			continue
		}
		if line.indent <= parentIndent {
			break
		}
		if strings.HasPrefix(line.text, "- ") {
			return nil, i, fmt.Errorf("unexpected list item at indent %d", line.indent)
		}
		key, rest, ok := splitSfcKeyValue(line.text)
		if !ok {
			return nil, i, fmt.Errorf("invalid mapping line: %q", line.text)
		}
		if rest == "|" || rest == ">" {
			val, next, err := parseSfcMultiline(lines, i+1, line.indent, rest == ">")
			if err != nil {
				return nil, i, err
			}
			result[key] = val
			i = next
			continue
		}
		if rest == "" {
			next := nextMeaningfulLine(lines, i+1)
			if next >= len(lines) || lines[next].indent <= line.indent {
				result[key] = make(map[string]any)
				i++
				continue
			}
			if strings.HasPrefix(lines[next].text, "- ") {
				val, ni, err := parseSfcList(lines, next, line.indent, depth+1)
				if err != nil {
					return nil, i, err
				}
				result[key] = val
				i = ni
				continue
			}
			val, ni, err := parseSfcMap(lines, next, line.indent, depth+1)
			if err != nil {
				return nil, i, err
			}
			result[key] = val
			i = ni
			continue
		}
		result[key] = parseSfcScalar(rest)
		i++
	}
	return result, i, nil
}

func parseSfcList(lines []yline, start int, parentIndent int, depth int) ([]any, int, error) {
	if depth > maxDepth {
		return nil, start, fmt.Errorf("max depth exceeded")
	}
	var out []any
	i := start
	for i < len(lines) {
		line := lines[i]
		if line.text == "" || strings.HasPrefix(line.text, "#") {
			i++
			continue
		}
		if line.indent <= parentIndent {
			break
		}
		if !strings.HasPrefix(line.text, "- ") {
			break
		}
		item := strings.TrimSpace(strings.TrimPrefix(line.text, "- "))
		if item == "" {
			next := nextMeaningfulLine(lines, i+1)
			if next < len(lines) && lines[next].indent > line.indent {
				if strings.HasPrefix(lines[next].text, "- ") {
					val, ni, err := parseSfcList(lines, next, line.indent, depth+1)
					if err != nil {
						return nil, i, err
					}
					out = append(out, val)
					i = ni
					continue
				}
				val, ni, err := parseSfcMap(lines, next, line.indent, depth+1)
				if err != nil {
					return nil, i, err
				}
				out = append(out, val)
				i = ni
				continue
			}
			out = append(out, "")
			i++
			continue
		}
		if k, rest, ok := splitSfcKeyValue(item); ok {
			m := map[string]any{k: parseSfcScalar(rest)}
			next := i + 1
			for next < len(lines) {
				nl := lines[next]
				if nl.text == "" || strings.HasPrefix(nl.text, "#") {
					next++
					continue
				}
				if nl.indent <= line.indent {
					break
				}
				kk, rr, ok := splitSfcKeyValue(nl.text)
				if !ok {
					return nil, next, fmt.Errorf("invalid list mapping line: %q", nl.text)
				}
				if rr == "|" || rr == ">" {
					val, ni, err := parseSfcMultiline(lines, next+1, nl.indent, rr == ">")
					if err != nil {
						return nil, next, err
					}
					m[kk] = val
					next = ni
					continue
				}
				if rr == "" {
					child := nextMeaningfulLine(lines, next+1)
					if child < len(lines) && lines[child].indent > nl.indent {
						if strings.HasPrefix(lines[child].text, "- ") {
							val, ni, err := parseSfcList(lines, child, nl.indent, depth+1)
							if err != nil {
								return nil, next, err
							}
							m[kk] = val
							next = ni
							continue
						}
						val, ni, err := parseSfcMap(lines, child, nl.indent, depth+1)
						if err != nil {
							return nil, next, err
						}
						m[kk] = val
						next = ni
						continue
					}
					m[kk] = make(map[string]any)
					next++
					continue
				}
				m[kk] = parseSfcScalar(rr)
				next++
			}
			out = append(out, m)
			i = next
			continue
		}
		out = append(out, parseSfcScalar(item))
		i++
	}
	return out, i, nil
}

func parseSfcMultiline(lines []yline, start int, parentIndent int, folded bool) (string, int, error) {
	if start >= len(lines) {
		return "", start, nil
	}
	base := -1
	var parts []string
	i := start
	for i < len(lines) {
		line := lines[i]
		if line.text == "" {
			parts = append(parts, "")
			i++
			continue
		}
		if line.indent <= parentIndent {
			break
		}
		if base == -1 {
			base = line.indent
		}
		raw := line.raw
		if len(raw) >= base {
			raw = raw[base:]
		}
		parts = append(parts, strings.TrimRight(raw, " "))
		i++
	}
	if folded {
		return strings.Join(compactFolded(parts), " "), i, nil
	}
	return strings.Join(parts, "\n"), i, nil
}

func compactFolded(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func nextMeaningfulLine(lines []yline, start int) int {
	for i := start; i < len(lines); i++ {
		if lines[i].text == "" || strings.HasPrefix(lines[i].text, "#") {
			continue
		}
		return i
	}
	return len(lines)
}

func splitSfcKeyValue(s string) (key, rest string, ok bool) {
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuote != 0 {
			if ch == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if ch == inQuote {
				inQuote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			inQuote = ch
			continue
		}
		if ch == ':' {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
		}
	}
	return "", "", false
}

func parseSfcScalar(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if s == "null" || s == "~" {
		return nil
	}
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		return parseSfcFlowSeq(s[1 : len(s)-1])
	}
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		return parseSfcFlowMap(s[1 : len(s)-1])
	}
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && strings.Contains(s, ".") {
		return f
	}
	return s
}

func parseSfcFlowSeq(s string) []any {
	parts := splitFlowItems(s)
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, parseSfcScalar(p))
		}
	}
	return out
}

func parseSfcFlowMap(s string) map[string]any {
	out := make(map[string]any)
	parts := splitFlowItems(s)
	for _, p := range parts {
		k, v, ok := splitSfcKeyValue(p)
		if ok {
			out[k] = parseSfcScalar(v)
		}
	}
	return out
}

func splitFlowItems(s string) []string {
	var out []string
	start := 0
	depth := 0
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuote != 0 {
			if ch == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if ch == inQuote {
				inQuote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			inQuote = ch
			continue
		}
		switch ch {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}
