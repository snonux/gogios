package main

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

type checkResult struct {
	name   string
	output string
	status int
}

func (c check) execute(ctx context.Context, name string) checkResult {
	cmd := exec.CommandContext(ctx, c.Plugin, c.Args...)

	var bytes bytes.Buffer
	cmd.Stdout = &bytes
	cmd.Stderr = &bytes

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return checkResult{
				name:   name,
				output: "Check command timed out",
				status: critical,
			}
		}
	}

	// Remove Nagios perf data from output and trim whitespaces
	parts := strings.Split(bytes.String(), "|")
	output := strings.TrimSpace(parts[0])

	return checkResult{
		name:   name,
		output: output,
		status: cmd.ProcessState.ExitCode(),
	}
}
