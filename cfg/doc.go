// Package cfg provides a minimal, high-performance configuration library
// with INI/YAML parsing, environment variable expansion, struct binding
// via reflection, and copy-on-write semantics for hot-reload support.
//
// Typical usage:
//
//	c := cfg.New()
//	c.LoadFile("config.cfg")
//	var app AppConfig
//	c.Bind("", &app)
package cfg
