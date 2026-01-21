package internal

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

type check struct {
	Plugin               string
	Args                 []string
	DependsOn            []string `json:"DependsOn,omitempty"`
	Retries              int      `json:"Retries,omitempty"`
	RetryInterval        int      `json:"RetryInterval,omitempty"`
	RunInterval          int      `json:"RunInterval,omitempty"`
	RandomSpread         int      `json:"RandomSpread,omitempty"`
	OnlyIfNotExists      string   `json:"OnlyIfNotExists,omitempty"`      // Suppress alerts if this file exists and is recent
	OnlyIfNotExistsMaxS  int      `json:"OnlyIfNotExistsMaxS,omitempty"`  // Max age in seconds for suppression file (uses global default if 0)
}

type namedCheck struct {
	check
	name string
}

type checkResult struct {
	name          string
	output        string
	epoch         int64
	status        nagiosCode
	federatedFrom string
}

// func (c checkResult) federated() bool {
// 	return c.federatedFrom != ""
// }

func (c check) run(ctx context.Context, name string) checkResult {
	cmd := exec.CommandContext(ctx, c.Plugin, c.Args...)

	var bytes bytes.Buffer
	cmd.Stdout = &bytes
	cmd.Stderr = &bytes

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return checkResult{name, "Check command timed out", time.Now().Unix(), nagiosCritical, ""}
		}
	}

	// Remove Nagios perf data from output and trim whitespaces
	parts := strings.Split(bytes.String(), "|")
	output := strings.TrimSpace(parts[0])

	ec := cmd.ProcessState.ExitCode()
	if ec < int(nagiosOk) || ec > int(nagiosUnknown) {
		// If the exit code is not in the range of known Nagios codes, treat it as unknown
		ec = int(nagiosUnknown)
	}

	return checkResult{name, output, time.Now().Unix(), nagiosCode(ec), ""}
}

func (c check) skip(name, output string) checkResult {
	return checkResult{name, output, time.Now().Unix(), nagiosUnknown, ""}
}

func (c namedCheck) run(ctx context.Context) checkResult {
	return c.check.run(ctx, c.name)
}

func (c namedCheck) skip(output string) checkResult {
	return c.check.skip(c.name, output)
}
