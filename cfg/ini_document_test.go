package cfg

import (
	"bytes"
	"strings"
	"testing"
)

const sampleINIDoc = `
; global comment
[DEFAULT]
key1=value1
key2= value2

[section1]
; comment inside section
multiline = """
hello
world
"""
quote1 = "a b c"
quote2 = 'x y z'

[section2]
tripleSingle = '''
aaa
bbb
ccc
'''
`

func TestINIDocument_ParseSave(t *testing.T) {
	doc := NewINIDocumentFromReader(strings.NewReader(sampleINIDoc))
	if err := doc.Parse(strings.NewReader(sampleINIDoc)); err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if doc.Data["DEFAULT"]["key1"] != "value1" {
		t.Fatalf("expected value1, got %q", doc.Data["DEFAULT"]["key1"])
	}
	if !strings.Contains(doc.Data["section1"]["multiline"], "hello\nworld") {
		t.Fatalf("multiline parse error: %q", doc.Data["section1"]["multiline"])
	}
	if doc.Data["section2"]["tripleSingle"] != "aaa\nbbb\nccc" {
		t.Fatalf("tripleSingle parse error: %q", doc.Data["section2"]["tripleSingle"])
	}
	var buf bytes.Buffer
	if err := doc.Save(&buf); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "hello\nworld") {
		t.Fatalf("save missing multiline:\n%s", out)
	}
	if !strings.Contains(out, "tripleSingle") {
		t.Fatalf("save missing tripleSingle:\n%s", out)
	}
	doc2 := NewINIDocumentFromReader(strings.NewReader(out))
	if err := doc2.Parse(strings.NewReader(out)); err != nil {
		t.Fatalf("reparse error: %v", err)
	}
	if doc2.Data["section1"]["quote1"] != "a b c" {
		t.Fatalf("quote1 mismatch after reparse: %q", doc2.Data["section1"]["quote1"])
	}
}

func TestINIDocument_CRUD(t *testing.T) {
	doc := NewINIDocumentFromReader(strings.NewReader("[sec1]\nkey1=val1\n"))
	if err := doc.Parse(strings.NewReader("[sec1]\nkey1=val1\n")); err != nil {
		t.Fatal(err)
	}
	doc.AddSection("sec2")
	if _, ok := doc.Data["sec2"]; !ok {
		t.Fatal("sec2 not added")
	}
	doc.SetKey("sec1", "newkey", "newval")
	if doc.Data["sec1"]["newkey"] != "newval" {
		t.Fatalf("set key failed: %#v", doc.Data["sec1"])
	}
	doc.RemoveKey("sec1", "key1")
	if _, ok := doc.Data["sec1"]["key1"]; ok {
		t.Fatal("key1 not removed")
	}
	doc.RemoveSection("sec1")
	if _, ok := doc.Data["sec1"]; ok {
		t.Fatal("sec1 not removed")
	}
	var buf bytes.Buffer
	if err := doc.Save(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "[sec2]") {
		t.Fatalf("output missing sec2: %s", buf.String())
	}
}

func TestINIDocument_CommentsPreserved(t *testing.T) {
	data := "\n; comment1\n[sec]\n; comment2\nkey = val\n"
	doc := NewINIDocumentFromReader(strings.NewReader(data))
	if err := doc.Parse(strings.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if doc.Data["sec"]["key"] != "val" {
		t.Fatalf("expected val, got %q", doc.Data["sec"]["key"])
	}
	var buf bytes.Buffer
	if err := doc.Save(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), ";comment1") {
		t.Fatalf("comment lost in output: %q", buf.String())
	}
}
