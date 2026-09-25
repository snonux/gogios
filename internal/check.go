package internal

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// checkWaitDelay bounds how long a check waits for its output pipes after the
// plugin was killed (see runCommand).
const checkWaitDelay = 5 * time.Second

type check struct {
	Plugin              string
	Args                []string
	DependsOn           []string `json:"DependsOn,omitempty"`
	Retries             int      `json:"Retries,omitempty"`
	RetryInterval       int      `json:"RetryInterval,omitempty"`
	RunInterval         int      `json:"RunInterval,omitempty"`
	RandomSpread        int      `json:"RandomSpread,omitempty"`
	OnlyIfNotExists     string   `json:"OnlyIfNotExists,omitempty"`     // Suppress alerts if this file exists and is recent
	OnlyIfNotExistsMaxS int      `json:"OnlyIfNotExistsMaxS,omitempty"` // Max age in seconds for suppression file (uses global default if 0)
	// Local marks a host-local check (disk, load, ... of the node Gogios
	// runs on). With peer failover a passive node still runs and notifies
	// for its Local checks, and the active node shows them from the peer's
	// report. Local checks should not depend on non-local ones.
	Local bool `json:"Local,omitempty"`
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
	out, ec, err := runCommand(ctx, checkWaitDelay, c.Plugin, c.Args...)
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return checkResult{name, "Check command timed out", time.Now().Unix(), nagiosCritical, ""}
	}

	// Remove Nagios perf data from output and trim whitespaces
	output := strings.TrimSpace(strings.Split(out, "|")[0])

	if ec < int(nagiosOk) || ec > int(nagiosUnknown) {
		// If the exit code is not in the range of known Nagios codes, treat it as unknown
		ec = int(nagiosUnknown)
	}

	return checkResult{name, output, time.Now().Unix(), nagiosCode(ec), ""}
}

// runCommand runs plugin and returns its combined output and exit code (-1
// when it did not exit normally). Killing the plugin when ctx ends is not
// enough to end the call: a child the plugin forked (a shell pipeline, a
// backgrounded helper) can hold stdout open, and without WaitDelay Wait blocks
// until that child exits, which kept a whole run, and with it the run lock,
// alive indefinitely. waitDelay caps the wait for the pipes after the kill.
func runCommand(ctx context.Context, waitDelay time.Duration, plugin string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, plugin, args...)
	cmd.WaitDelay = waitDelay

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	return buf.String(), cmd.ProcessState.ExitCode(), err
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
