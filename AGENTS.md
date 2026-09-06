# Gogios - Agent Guidelines

## Project Overview

Gogios is a lightweight, minimalistic monitoring tool written in Go. It executes Nagios/Icinga monitoring plugins and sends email notifications on status changes.

## Commands

### Build
```bash
mage build          # Build the gogios binary
go build -o gogios cmd/gogios/main.go  # Alternative without mage
```

### Development Build (with race detector)
```bash
mage dev            # Runs vet + lint, then builds with -race
```

### Test
```bash
mage test           # Run all unit tests (clears test cache first)
go test ./...       # Alternative without mage
```

### Lint & Vet
```bash
mage vet            # Run go vet
mage lint           # Run golangci-lint
mage lintInstall    # Install golangci-lint
```

### Build, package, and deploy for OpenBSD/FreeBSD
Packaging targets were removed from the Magefile; cross-compiling, packaging,
signing, and uploading to the custom package repo now live in
`~/git/conf/packages/Makefile` (outside this repo):
```bash
cd ~/git/conf/packages && make pkg-openbsd NAME=gogios SRC=/home/paul/git/gogios
cd ~/git/conf/packages && make pkg-freebsd NAME=gogios SRC=/home/paul/git/gogios
```
Installing onto the frontends is then done via `rex gogios_install` from
`~/git/conf/frontends` (see the `gogios` skill for the full workflow).

## Project Structure

```
cmd/gogios/         # Main entry point
internal/           # Core implementation
  check.go          # Check execution logic
  config.go         # Configuration parsing
  dependency.go     # Check dependency handling
  federated.go      # Federated monitoring
  html.go           # HTML report generation
  nagioscode.go     # Nagios exit code handling
  notify.go         # Email notification
  run.go            # Main run logic
  runchecks.go      # Check orchestration
  state.go          # State persistence
```

## Code Conventions

- Go 1.24+
- Use standard Go formatting (`gofmt`)
- Tests use the standard `testing` package with `*_test.go` suffix
- Internal packages under `internal/` are not exported
- Module path: `github.com/snonux/gogios`

## Testing

Tests exist in `internal/` with the `*_test.go` naming convention:
- `federated_test.go`
- `html_test.go`
- `state_test.go`

Run tests before committing changes.

For best practices also follow ~/git/conf/snippets/go/go-projects/go-projects.md if present.

For deployment and testing procedures, see ~/Notes/snippets/go-projects/gogios-deploy-and-test.md

For alert troubleshooting and common issues, see ~/Notes/snippets/go-projects/gogios-troubleshoot-alert.md
