package main

import (
	"bytes"
	"context"
	"os/exec"
)

type check struct {
	Plugin string
	Args   []string
}

func (c check) execute(ctx context.Context) (string, int) {
	cmd := exec.CommandContext(ctx, c.Plugin, c.Args...)

	var bytes bytes.Buffer
	cmd.Stdout = &bytes
	cmd.Stderr = &bytes

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "Check command timed out", critical
		}
	}

	return bytes.String(), cmd.ProcessState.ExitCode()
}
