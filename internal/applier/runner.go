package applier

import (
	"bytes"
	"context"
	"os/exec"
)

// commandRunner is the seam between this package and the outside world for
// both external processes it invokes: the tproxy-server `-check` validator
// and the privileged `sudo apply-profiles.sh` call. Neither can run for
// real in unit tests (no tproxy-server binary, no sudo, on a macOS dev
// machine) — swap in a fake in tests, execCommandRunner in production.
type commandRunner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
