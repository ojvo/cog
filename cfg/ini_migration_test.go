package cfg

import (
	"bytes"
	"os"
	"sync"
	"testing"
)

func TestParseINI_ColonSeparator(t *testing.T) {
	c, err := Parse([]byte("host: localhost\nport: 8080\n"), FormatINI)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.StringOr("host", ""); v != "localhost" {
		t.Fatalf("host=%q", v)
	}
	if v := c.IntOr("port", 0); v != 8080 {
		t.Fatalf("port=%d", v)
	}
}

func TestParseINI_MultilineTripleQuote(t *testing.T) {
	input := "[doc]\nbody = \"\"\"\nhello\nworld\n\"\"\"\n"
	c, err := Parse([]byte(input), FormatINI)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.StringOr("doc.body", ""); v != "hello\nworld" {
		t.Fatalf("body=%q", v)
	}
}

func TestParseINI_ContinuationAndInlineComment(t *testing.T) {
	input := "path = C:/app/\\\nconfig ; ignored\nmsg = \"a;b#c\" ; tail\n"
	c, err := Parse([]byte(input), FormatINI)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.StringOr("path", ""); v != "C:/app/config" {
		t.Fatalf("path=%q", v)
	}
	if v := c.StringOr("msg", ""); v != "a;b#c" {
		t.Fatalf("msg=%q", v)
	}
}

func TestParseINI_BOM(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte("name = cog\n")...)
	c, err := Parse(data, FormatINI)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.StringOr("name", ""); v != "cog" {
		t.Fatalf("name=%q", v)
	}
}

func TestINIDocument_BOMAndColon(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte("[sec]\nkey: value\n")...)
	doc := NewINIDocumentFromReader(bytes.NewReader(data))
	if err := doc.Parse(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if got := doc.Data["sec"]["key"]; got != "value" {
		t.Fatalf("got %q", got)
	}
}

func TestConfig_CoWConcurrentExpandEnvRead(t *testing.T) {
	os.Setenv("COG_CFG_HOST", "prod")
	defer os.Unsetenv("COG_CFG_HOST")
	c, err := Parse([]byte(`{"host":"${COG_CFG_HOST}","port":8080}`), FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 300; j++ {
				_ = c.StringOr("host", "")
				_ = c.ExpandEnv().StringOr("host", "")
			}
		}()
	}
	wg.Wait()
}
