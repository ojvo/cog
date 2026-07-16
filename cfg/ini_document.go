package cfg

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// INIDocument preserves section/key order, comments, blank lines, and multiline values.
type INIDocument struct {
	Data   map[string]map[string]string
	Nodes  []*ININode
	curSec string
}

type ININodeType int

const (
	ININodeSection ININodeType = iota
	ININodeKeyValue
	ININodeComment
	ININodeEmpty
)

type ININode struct {
	Type      ININodeType
	Section   string
	Key       string
	Value     string
	Multiline bool
	Comment   string
}

func NewINIDocumentFromReader(r io.Reader) *INIDocument {
	return &INIDocument{Data: make(map[string]map[string]string), Nodes: []*ININode{}}
}

func NewINIDocumentFromFile(path string) (*INIDocument, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	doc := NewINIDocumentFromReader(f)
	return doc, doc.Parse(f)
}

func (d *INIDocument) Parse(r io.Reader) error {
	d.Data = make(map[string]map[string]string)
	d.Nodes = d.Nodes[:0]
	d.curSec = ""
	br := bufio.NewReader(r)
	if bom, _ := br.Peek(3); len(bom) == 3 && bom[0] == 0xEF && bom[1] == 0xBB && bom[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil && len(line) == 0 {
			if err == io.EOF {
				return nil
			}
			return err
		}
		str := strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(str)
		if trimmed == "" {
			d.Nodes = append(d.Nodes, &ININode{Type: ININodeEmpty})
			if err == io.EOF {
				return nil
			}
			continue
		}
		switch trimmed[0] {
		case ';', '#':
			d.Nodes = append(d.Nodes, &ININode{Type: ININodeComment, Comment: strings.TrimSpace(trimmed[1:])})
			if err == io.EOF {
				return nil
			}
			continue
		case '[':
			if idx := strings.Index(trimmed, "]"); idx != -1 {
				sec := strings.TrimSpace(trimmed[1:idx])
				d.curSec = sec
				if _, ok := d.Data[sec]; !ok {
					d.Data[sec] = make(map[string]string)
				}
				d.Nodes = append(d.Nodes, &ININode{Type: ININodeSection, Section: sec})
				if err == io.EOF {
					return nil
				}
				continue
			}
		}
		if idx := firstINISeparator(str); idx != -1 {
			key := strings.TrimSpace(str[:idx])
			val := strings.TrimSpace(str[idx+1:])
			if strings.HasPrefix(val, `"""`) || strings.HasPrefix(val, `'''`) {
				quote := val[:3]
				val = strings.TrimPrefix(val, quote)
				var lines []string
				if strings.HasSuffix(val, quote) {
					val = strings.TrimSuffix(val, quote)
					lines = append(lines, val)
				} else {
					if val != "" {
						lines = append(lines, val)
					}
					for {
						l, e2 := br.ReadString('\n')
						clean := strings.TrimRight(l, "\r\n")
						if strings.HasSuffix(strings.TrimSpace(clean), quote) {
							end := strings.LastIndex(clean, quote)
							content := clean[:end]
							if content != "" {
								lines = append(lines, content)
							}
							if e2 != nil && e2 != io.EOF {
								return e2
							}
							break
						}
						lines = append(lines, clean)
						if e2 == io.EOF {
							return fmt.Errorf("cfg: ini document parse error: unclosed multiline value for key %s", key)
						}
					}
				}
				val = strings.Join(lines, "\n")
			} else {
				val = stripINIInlineComment(val)
				val = iniUnquoteValue(val)
			}
			sec := d.curSec
			if sec == "" {
				sec = "DEFAULT"
				if _, ok := d.Data[sec]; !ok {
					d.Data[sec] = make(map[string]string)
				}
			}
			d.Data[sec][key] = val
			d.Nodes = append(d.Nodes, &ININode{Type: ININodeKeyValue, Section: sec, Key: key, Value: val, Multiline: strings.Contains(val, "\n")})
			if err == io.EOF {
				return nil
			}
			continue
		}
		d.Nodes = append(d.Nodes, &ININode{Type: ININodeComment, Comment: "WARN: unexpected value: " + str})
		if err == io.EOF {
			return nil
		}
	}
}

func iniUnquoteValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		s = s[1 : len(s)-1]
	}
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}

func (d *INIDocument) Save(w io.Writer) error {
	bw := bufio.NewWriter(w)
	curSec := ""
	for _, node := range d.Nodes {
		switch node.Type {
		case ININodeSection:
			curSec = node.Section
			if _, err := fmt.Fprintf(bw, "[%s]\n", curSec); err != nil {
				return err
			}
		case ININodeKeyValue:
			val := d.Data[curSec][node.Key]
			if node.Section == "DEFAULT" && curSec == "" {
				val = d.Data["DEFAULT"][node.Key]
			}
			if node.Multiline {
				if _, err := fmt.Fprintf(bw, "%s = \"\"\"\n%s\n\"\"\"\n", node.Key, val); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintf(bw, "%s = %s\n", node.Key, iniQuoteIfNeeded(val)); err != nil {
					return err
				}
			}
		case ININodeComment:
			if _, err := fmt.Fprintf(bw, ";%s\n", node.Comment); err != nil {
				return err
			}
		case ININodeEmpty:
			if _, err := fmt.Fprintln(bw); err != nil {
				return err
			}
		}
	}
	return bw.Flush()
}

func iniQuoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	if iniNeedsQuotes(s) {
		return fmt.Sprintf("%q", s)
	}
	return s
}

func iniNeedsQuotes(s string) bool {
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '"' || r == '\'' || r == ';' || r == '#' || r == '=' || r == ':' {
			return true
		}
	}
	return false
}

func (d *INIDocument) AddSection(name string) {
	if _, ok := d.Data[name]; ok {
		return
	}
	d.Data[name] = make(map[string]string)
	d.Nodes = append(d.Nodes, &ININode{Type: ININodeSection, Section: name})
}

func (d *INIDocument) RemoveSection(name string) {
	delete(d.Data, name)
	newNodes := make([]*ININode, 0, len(d.Nodes))
	skip := false
	for _, n := range d.Nodes {
		if n.Type == ININodeSection && n.Section == name {
			skip = true
			continue
		}
		if skip && n.Type == ININodeSection {
			skip = false
		}
		if !skip {
			newNodes = append(newNodes, n)
		}
	}
	d.Nodes = newNodes
}

func (d *INIDocument) SetKey(sec, key, val string) {
	if _, ok := d.Data[sec]; !ok {
		d.AddSection(sec)
	}
	d.Data[sec][key] = val
	found := false
	curSec := ""
	for _, n := range d.Nodes {
		if n.Type == ININodeSection {
			curSec = n.Section
		}
		if curSec == sec && n.Type == ININodeKeyValue && n.Key == key {
			n.Value = val
			n.Multiline = strings.Contains(val, "\n")
			found = true
			break
		}
	}
	if !found {
		for i := range d.Nodes {
			if d.Nodes[i].Type == ININodeSection && d.Nodes[i].Section == sec {
				idx := i + 1
				newNode := &ININode{Type: ININodeKeyValue, Section: sec, Key: key, Value: val, Multiline: strings.Contains(val, "\n")}
				d.Nodes = append(d.Nodes[:idx], append([]*ININode{newNode}, d.Nodes[idx:]...)...)
				return
			}
		}
	}
}

func (d *INIDocument) RemoveKey(sec, key string) {
	delete(d.Data[sec], key)
	newNodes := make([]*ININode, 0, len(d.Nodes))
	curSec := ""
	for _, n := range d.Nodes {
		if n.Type == ININodeSection {
			curSec = n.Section
		}
		if curSec == sec && n.Type == ININodeKeyValue && n.Key == key {
			continue
		}
		newNodes = append(newNodes, n)
	}
	d.Nodes = newNodes
}
