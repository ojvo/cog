// Package cfg provides a minimal, high-performance configuration library
// with INI/YAML parsing, environment variable expansion, struct binding
// via reflection, and copy-on-write semantics for hot-reload support.
//
// It also provides an INI document model for preserving comments, ordering,
// blank lines, and multiline values during read-modify-write workflows.
//
// Typical usage:
//
//	c := cfg.New()
//	c.LoadFile("config.cfg")
//	var app AppConfig
//	c.Bind("", &app)
package cfg
