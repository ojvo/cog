// Package env provides OS-environment interaction helpers, analogous
// to the role the standard library "os" package plays for the Go
// runtime. It groups facilities that query or mutate the process's
// surrounding environment:
//
//   - executable and process environment paths, working directory, user
//     home directory, and tilde-expansion helpers (exec.go)
//   - timezone parsing
//   - fast cached time source for high-frequency reads
//   - lightweight platform-specific disk info
//   - Linux runtime statistics: CPU, memory, network, and per-process
//     resource usage from /proc (stat_linux.go)
//   - file-system operations: existence/metadata checks, directory
//     listing and traversal, copy/move/remove, atomic write helpers,
//     path manipulation, and file-content hashing (file.go)
package env
