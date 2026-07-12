// Package cor provides a Kernel facade that manages component lifecycle
// (initialization, startup, shutdown) and config-driven wiring of cog
// subpackages (cfg, log). It offers shortcut logging functions that
// delegate to the configured logger.
//
// Typical usage:
//
//	if err := cor.Init("config.cfg"); err != nil { ... }
//	defer cor.Close()
//	cor.Info("started")
package cor
