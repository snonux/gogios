# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Gogios is a lightweight, minimalistic monitoring tool written in Go designed for small-scale server monitoring. It executes Nagios-compatible check plugins and sends email notifications when service states change.

## Build and Development Commands

### Build
```bash
# Standard build
go build -o gogios cmd/gogios/main.go

# Using taskfile
task build

# Development build with race detection
task dev
```

### Code Quality
```bash
# Run go vet
task vet

# Run linter (requires golangci-lint)
task lint

# Install linter
task lint-install
```

## Architecture

### Core Components

- **cmd/gogios/main.go**: Entry point with CLI argument parsing
- **internal/run.go**: Main execution orchestrator
- **internal/config.go**: JSON configuration file parsing and validation
- **internal/check.go**: Individual check execution using external Nagios plugins
- **internal/state.go**: Persistent state management (tracks check status changes)
- **internal/runchecks.go**: Concurrent check execution with dependency handling
- **internal/notify.go**: Email notification system
- **internal/federated.go**: Federation support for distributed monitoring
- **internal/nagioscode.go**: Nagios exit code constants (OK, WARNING, CRITICAL, UNKNOWN)
- **internal/dependency.go**: Check dependency resolution

### Key Design Patterns

1. **State-based Notifications**: Only sends emails when check status changes, not on every run
2. **Dependency Checks**: Supports check dependencies (e.g., don't check HTTP if ping fails)
3. **Concurrent Execution**: Runs multiple checks simultaneously with configurable concurrency
4. **Retry Logic**: Configurable retry attempts with intervals for failed checks
5. **Federation**: Can merge results from multiple Gogios instances
6. **Stale Detection**: Identifies checks that haven't run within threshold time

### Configuration

The tool is configured via JSON file (typically `/etc/gogios.json`) with:
- Email settings (from/to addresses, SMTP server)
- Check definitions (plugin path, arguments, dependencies)
- Concurrency and timeout settings
- State persistence directory

### Execution Flow

1. Load configuration and validate check dependencies
2. Load previous state from state.json
3. Execute checks concurrently (respecting dependencies)
4. Merge federated results if configured
5. Generate report and send notifications for status changes
6. Persist new state

## Development Notes

- Uses standard Go project structure with internal package
- No external dependencies beyond Go standard library
- Designed for Unix-like systems (tested on OpenBSD)
- Supports cross-compilation for different platforms
- State persisted as JSON in configurable directory