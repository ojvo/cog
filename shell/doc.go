// Package shell provides a cross-platform command execution abstraction
// with timeout/context support, streaming output, and workflow orchestration.
//
// # Core types
//
//   - Cmd: async command runner with option-chainable configuration
//   - OutputStream / OutputBuffer: line-aware streaming collectors
//   - CommandQueue / WorkflowStateMachine: multi-step command orchestration
//   - Commander: minimal ICommander interface for mocking
//
// # Quick start
//
//	out, code, err := shell.Command("echo hello")
//	out, code, err := shell.CommandContext(ctx, "sleep 5")
//
//	cmd := shell.NewCommand("go build ./...", shell.WithTimeout(60))
//	if err := cmd.Run(); err != nil { ... }
//	fmt.Println(cmd.Status.Output)
package shell
