package shell

import "context"

// ICommander is an interface for running commands, useful for mocking in tests.
type ICommander interface {
	CommandWithResult() (string, error)
	CommandWithResultContext(ctx context.Context) (string, error)
}

// Commander is a concrete ICommander that runs commands in exec mode.
type Commander struct {
	Command string
	Args    []string
}

// CommandWithResult runs the command and returns its stdout.
func (r Commander) CommandWithResult() (string, error) {
	return ExecuteCommand(r.Command, r.Args...)
}

// CommandWithResultContext runs the command with a context and returns its stdout.
func (r Commander) CommandWithResultContext(ctx context.Context) (string, error) {
	cmd := NewCommand(r.Command, WithExecMode(true), WithArgs(r.Args...), WithContext(ctx))
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return cmd.Status.Stdout, nil
}
