package internal

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

type check struct {
	Plugin string
	Args   []string
}

type namedCheck struct {
	check
	name string
}

type checkResult struct {
	name   string
	output string
	status nagiosCode
}

func (c check) run(ctx context.Context, name string) checkResult {
	cmd := exec.CommandContext(ctx, c.Plugin, c.Args...)

	var bytes bytes.Buffer
	cmd.Stdout = &bytes
	cmd.Stderr = &bytes

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return checkResult{name, "Check command timed out", critical}
		}
	}

	// Remove Nagios perf data from output and trim whitespaces
	parts := strings.Split(bytes.String(), "|")
	output := strings.TrimSpace(parts[0])

	return checkResult{name, output, nagiosCode(cmd.ProcessState.ExitCode())}
}

func (c namedCheck) run(ctx context.Context) checkResult {
	return c.check.run(ctx, c.name)
}
