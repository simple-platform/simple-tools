<h1 align="center">Simple CLI</h1>

<p align="center">
  <strong>The command-line interface for the Simple Platform</strong><br>
  <em>Build, test, and deploy enterprise applications</em>
</p>

<p align="center">
  <a href="../../LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License"></a>
  <a href="#installation"><img src="https://img.shields.io/badge/go-%3E%3D1.25.5-00ADD8.svg" alt="Go Version"></a>
  <a href="#contributing"><img src="https://img.shields.io/badge/PRs-welcome-brightgreen.svg" alt="PRs Welcome"></a>
</p>

---

## Overview

**Simple CLI** (`simple`) is the official tool for managing Simple Platform projects. It allows you to:

- Initialize new workspaces
- Scaffold applications and actions

---

## Installation

### Automatic Install (Recommended)

Run the following command to install `simple` on macOS, Linux, or Windows (Git Bash):

```bash
curl -fsSL https://tools.simple.dev/simple-cli/install | bash
```

### Install from Source

**Prerequisites**: Go 1.25.5 or later ([Download](https://golang.org/dl/))

```bash
# Clone the repository
git clone https://github.com/simple-platform/simple-tools.git
cd simple-tools/tools/simple-cli

# Build the binary
go build -o simple ./cmd/simple

# (Optional) Install to PATH
sudo mv simple /usr/local/bin/
```

### Verify Installation

```bash
simple --help
```

---

## Quick Start

```bash
# 1. Initialize a new workspace
simple init my-project && cd my-project

# 2. Create an application
simple new app com.mycompany.crm "Customer CRM"

# 3. Create a server-side action
simple new action com.mycompany.crm send-email "Send Email" \
  --scope mycompany \
  --env server
```

---

## Command Reference

### Global Flags

| Flag         | Description                                           |
| ------------ | ----------------------------------------------------- |
| `--json`     | Output results in JSON format (useful for scripts/CI) |
| `-h, --help` | Show help for any command                             |

---

### `simple build`

Build all actions within an application.

**Usage:**

```bash
simple build [app-path] [flags]
```

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `app-path` | No | Path to the application directory. Defaults to current directory. |

**Flags:**
| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--concurrency` | `-c` | `NumCPU` | Number of concurrent build workers. |
| `--verbose` | `-v` | `true` | Enable verbose output. |
| `--json` | | `false` | Output build results in JSON. |

**Examples:**

```bash
# Build current app
simple build

# Build specific app with custom concurrency
simple build apps/com.company.crm --concurrency 8
```

---

### `simple test`

Run tests for applications, actions, or record behaviors.

Each target runs under its own test runner: TypeScript and JavaScript under
Vitest, Rust actions under `cargo test`. A Rust action's tests run on this
machine against the SDK's test seam, so they need no wasm build and no emulator.

**Usage:**

```bash
simple test [app-id] [flags]
```

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `app-id` | No | Target app ID. If omitted, runs all tests in the workspace. |

**Flags:**
| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--action` | `-a` | - | Run tests for a specific action. |
| `--behavior` | `-b` | - | Run tests for a specific record behavior. |
| `--coverage` | | `false` | Enable code coverage reporting. Vitest targets only; Rust coverage is a separate tool (`cargo-llvm-cov`), so Rust actions run without it and the run says so. |
| `--json` | | `false` | Output results in JSON format. |

**Examples:**

```bash
# Run all tests
simple test

# Run all tests for an app
simple test com.mycompany.crm

# Test specific action
simple test com.mycompany.crm --action send-email

# Test behavior
simple test com.mycompany.crm --behavior order
```

---

### `simple deploy`

Deploy an application to an environment: bump the version in `app.scl`, upload the files the server does not have yet, publish the version, and install it.

**Usage:**

```bash
simple deploy <app-path> --env <environment> [flags]
```

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `app-path` | Yes | Path to the application directory, e.g. `apps/com.mycompany.crm`. |

**Flags:**
| Flag | Default | Description |
|------|---------|-------------|
| `--env` | _(required)_ | Target environment (`dev`, `staging`, `prod`). |
| `--bump` | - | `patch`, `minor` or `major`. Required for the first deploy after a prod release. |
| `--dry-run` | `false` | Show the version and the files a deploy would upload, and change nothing (see below). |
| `--no-install` | `false` | Publish the version without installing it. |
| `--progress` | `auto` | How progress is shown: `auto`, `tty` or `plain` (see below). |
| `--json` | `false` | Print one JSON document instead of progress (see below). |

**Progress output:**

A deploy runs as a list of steps: load project config, authenticate, bump version, collect files, connect, compare with server, upload files, publish version, install. Progress goes to stdout.

- **On a terminal** the list is drawn live and repainted in place: every step is shown, pending ones included, with a spinner, its duration, and a progress bar while files are collected and uploaded. When the deploy ends, the finished list stays on screen, followed by the result line.
- **Anywhere else** (a pipe, a file, a CI log) each event is one plain line with no escape sequences: `[n/N] Step` when a step starts, then `✓` done, `✗` failed, `-` skipped or `■` interrupted, with the step's detail and duration. Upload progress is printed at most every 10 seconds, and a step that has printed nothing for 30 seconds prints `still running (<duration>)`, so CI jobs do not time out on silence.
- `auto` picks the live view only when stdout is a terminal, `TERM` is not `dumb` and `CI` is unset, `false` or `0`. `--progress=tty` or `--progress=plain` overrides that choice. `--json` shows no progress at all.
- With `CI` set to `false` or `0`, the live view is drawn without colour: the colour library treats any non-empty `CI` as a non-terminal.

```text
🚀 Deploying apps/com.acme.crm to dev
[1/9] Load project config
[1/9] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
...
[7/9] Upload files
      104/312 files · 4.1/12.4 MB (33%)
      208/312 files · 8.3/12.4 MB (66%)
[7/9] ✓ Upload files: 312 files · 12.4 MB (28s)
[8/9] Publish version
[8/9] ✓ Publish version: com.acme.crm@1.4.3-dev.5 (18.3s)
[9/9] Install to dev
      still running (30s)
      still running (1m00s)
      still running (1m30s)
[9/9] ✓ Install to dev: 1.4.3-dev.5 (1m52s)
✅ Deployed com.acme.crm@1.4.3-dev.5 (Installed) in 3m21.86s
```

**Failures and interrupts:** when a deploy fails or is interrupted, it says what it left behind before the error: `app.scl` already bumped on disk, or a publish or install the server may still finish after the CLI stopped waiting, and the `simple install` command to run next. Ctrl+C or `SIGTERM` stops the deploy gracefully: the current step gets up to 2 seconds to stop (on a terminal, `Waiting for "<step>" to stop` shows while it does), and the error names where the interrupt stopped it, e.g. `deploy interrupted during "Upload files" after 4.2s`. A second Ctrl+C exits at once. The exit code is 1. On a terminal, Ctrl+Z suspends the deploy as usual.

**Dry run:** `--dry-run` loads `simple.scl` and `app.scl`, computes the version a deploy would bump to, collects the files and lists them, sorted by path. It does not sign in (the environment's API key need not be set), does not connect to the server and does not write `app.scl`, so the listed `app.scl` hash is the file as it is on disk; a real deploy uploads it with the new version.

**JSON output (`--json`):** stdout carries at most one JSON document and nothing else, and no progress is printed. A `.env` file that fails to load is still reported, as a plain `warning: ...` line on stderr. Errors go to stderr as `{"error": "..."}` and the exit code is 1.

| Outcome                           | stdout                                                                                                                                               | Exit code |
| --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- | --------- |
| Success                           | `{"status":"success","app_id","version","files":{"total","new","cached"},"duration_ms"}`, plus `"installed","install_success"` unless `--no-install` | 0         |
| Published, install failed         | `{"status":"error","error":"deploy successful but install failed: ...","app_id","version"}`                                                          | 1         |
| Dry run                           | `{"dry_run":true,"version","files":[{"path","hash","size"}]}`, files sorted by path                                                                  | 0         |
| Any other failure or an interrupt | nothing                                                                                                                                              | 1         |

When the connection drops or the reply times out during the install, the server may still be installing, and the error reads `deploy successful but the install result is unknown: ...` instead.

**Examples:**

```bash
# First deploy to dev after a prod release
simple deploy apps/com.mycompany.crm --env dev --bump patch

# Preview what would be uploaded
simple deploy apps/com.mycompany.crm --env dev --dry-run

# Plain progress lines, e.g. under a terminal-emulating CI runner
simple deploy apps/com.mycompany.crm --env staging --progress=plain
```

---

### `simple install`

Install the latest deployed version of an application in an environment (migrations, service configuration, cache warming). `simple deploy` installs by default; use this after `--no-install`, or to retry an install that failed.

**Usage:**

```bash
simple install <app-id> --env <environment> [flags]
```

**Flags:**
| Flag | Default | Description |
|------|---------|-------------|
| `--env` | _(required)_ | Target environment (`dev`, `staging`, `prod`). |
| `--progress` | `auto` | How progress is shown: `auto`, `tty` or `plain`, as for `simple deploy`. |
| `--json` | `false` | Print `{"status":"success","app_id","version","env","duration_ms"}` on success and nothing else on stdout; errors go to stderr as `{"error": "..."}`. |

The steps are load project config, authenticate, connect, and install. The server does not cancel an install when the CLI disconnects: after an interrupt, or a dropped connection, let it finish before running `simple install` again.

---

### `simple auth`

Manages Proof-of-Possession (PoP) machine authentication for the Simple Platform.

---

#### `simple auth status`

Display currently enrolled cryptographic keypairs and cached JWT sessions.

**Usage:**

```bash
simple auth status
```

**Flags:**
| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | Emit output as JSON for automation. |

---

#### `simple auth enroll`

Generate a machine keypair and enroll it with the Identity service.

**Usage:**

```bash
simple auth enroll --env <environment>
```

**Flags:**
| Flag | Default | Description |
|------|---------|-------------|
| `--env` | _(required)_ | Target environment (`dev`, `staging`, `prod`). |
| `--json` | `false` | Emit output as JSON for automation. |

---

#### `simple auth logout`

Clear the cached session token for an environment. The next deploy or install will re-authenticate automatically.

**Usage:**

```bash
simple auth logout --env <environment>
```

**Flags:**
| Flag | Default | Description |
|------|---------|-------------|
| `--env` | _(required)_ | Target environment (`dev`, `staging`, `prod`). |
| `--json` | `false` | Emit output as JSON for automation. |

---

### `simple init`

Initialize a new Simple Platform workspace.

**Usage:**

```bash
simple init <path>
```

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | The directory to initialize. Can be `.` for current directory or a new folder name. |

**Examples:**

```bash
# Create a new project in a new folder
simple init my-new-project

# Initialize in current directory
simple init .
```

---

### `simple new app`

Create a new application within the workspace.

**Usage:**

```bash
simple new app <app-id> <name> [flags]
```

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `app-id` | Yes | Unique reverse-domain identifier (e.g., `com.mycompany.crm`). |
| `name` | Yes | Human-readable display name (e.g., "Customer CRM"). |

**Flags:**
| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--desc` | `-d` | `""` | A brief description of the application. |

**Examples:**

```bash
# Create a CRM app
simple new app com.mycompany.crm "Customer CRM"

# Create with description
simple new app com.mycompany.inventory "Inventory System" \
  --desc "Manages warehouses and stock"
```

---

### `simple new action`

Scaffold a new TypeScript action inside an application.

**Usage:**

```bash
simple new action <app> <name> <display_name> [flags]
```

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `app` | Yes | The ID of the target application (must exist in `apps/`). |
| `name` | Yes | The action name in kebab-case (e.g., `send-email`). |
| `display_name` | Yes | Human-readable name for the UI (e.g., "Send Email"). |

**Flags:**
| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--scope` | `-s` | Required for `--lang ts` | The NPM scope for the package (without `@`). A Rust action's crate is named after the action and is never published to a registry, so it takes no scope. |
| `--env` | `-e` | `server` | Execution environment: `server`, `client`, or `both`. |
| `--desc` | `-d` | `""` | Description of the action. |
| `--lang` | `-l` | `ts` | Programming language: `ts` or `rust`. |

**Examples:**

```bash
# Create a server-side action
simple new action com.mycompany.crm send-invite "Send Invite" \
  --scope mycompany \
  --env server

# Create a client-side action (e.g., for UI logic)
simple new action com.mycompany.crm validate-form "Validate Form" \
  --scope mycompany \
  --env client

# Create a Rust action (a cargo crate; no NPM scope)
simple new action com.mycompany.crm close-lead "Close Lead" \
  --lang rust \
  --env server
```

**Making an action callable by an agent:** a scaffolded action is not a tool. It
becomes one when its doc comment says so, with the `@tool`, `@effects`, `@retry`
and `@discloses` tags the build carries into `action.json` — see
[the action exposure vocabulary](docs/action-exposure-vocabulary.md).

---

### `simple new behavior`

Create a new record behavior script and register it in SCL.

**Usage:**

```bash
simple new behavior <app-id> <table-name>
```

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `app-id` | Yes | Target App ID. |
| `table-name` | Yes | Name of the table to attach behavior to (e.g., `order`). |

**Examples:**

```bash
simple new behavior com.mycompany.crm order
```

---

### `simple new trigger`

Create a new trigger that invokes an existing action.

**Usage:**

```bash
simple new trigger:<type> <app> <name> <display_name> --action <action_name> [flags]
```

**Types:**

#### 1. Timed Trigger (`trigger:timed`)

Runs an action on a schedule.

```bash
simple new trigger:timed <app> <name> <display_name> [flags]
```

**Flags:**
| Flag | Required | Default | Description | Example |
|------|----------|---------|-------------|---------|
| `--action` | Yes | - | The action to link this trigger to. | `--action sync-data` |
| `--frequency` | Yes | - | Schedule frequency: `minutely`, `hourly`, `daily`, `weekly`, `monthly`, `yearly` | `--frequency daily` |
| `--interval` | No | `1` | Number of periods between runs. | `--interval 2` (every 2 days) |
| `--time` | No | `00:00:00` | Time of day to run (HH:MM:SS). | `--time 14:30:00` |
| `--timezone` | No | `UTC` | IANA Timezone. | `--timezone America/New_York` |
| `--days` | No | - | Specific days (MON-SUN). | `--days MON,WED,FRI` |
| `--weekdays` | No | `false` | Run Mon-Fri. | `--weekdays` |
| `--weekends` | No | `false` | Run Sat-Sun. | `--weekends` |
| `--week-of-month` | No | - | For monthly: `first`, `second`, `third`, `fourth`, `fifth`, `last`. | `--week-of-month first` |
| `--start-at` | No | - | ISO8601 start time. | `--start-at 2024-01-01T00:00:00Z` |
| `--end-at` | No | - | ISO8601 end time. | `--end-at 2024-12-31T23:59:59Z` |
| `--on-overlap` | No | `skip` | overlap policy: `skip`, `queue`, `allow`. | `--on-overlap queue` |

**Examples:**

_Daily at 9 AM New York time:_

```bash
simple new trigger:timed com.company.crm daily-sync "Daily Sync" \
  --action sync-data \
  --frequency daily \
  --time "09:00:00" \
  --timezone "America/New_York"
```

_First Monday of every month:_

```bash
simple new trigger:timed com.company.crm monthly-review "Monthly Review" \
  --action run-review \
  --frequency monthly \
  --days MON \
  --week-of-month first
```

---

#### 2. Database Trigger (`trigger:db`)

Fires when a database record is created, updated, or deleted.

```bash
simple new trigger:db <app> <name> <display_name> [flags]
```

**Flags:**
| Flag | Required | Default | Description | Example |
|------|----------|---------|-------------|---------|
| `--action` | Yes | - | The action to link. | `--action process-order` |
| `--table` | Yes | - | Database table to watch. | `--table orders` |
| `--ops` | No | `insert` | Comma-separated operations: `insert`, `update`, `delete`. | `--ops insert,update` |
| `--condition` | No | - | JQ condition for the event. | `--condition '.record.status == "pending"'` |

**Example:**

_Trigger on order creation or update:_

```bash
simple new trigger:db com.company.crm on-order "On Order" \
  --action process-order \
  --table order \
  --ops insert,update \
  --condition '.record.status == "pending"'
```

---

#### 3. Webhook Trigger (`trigger:webhook`)

Creates an HTTP endpoint that triggers the action.

```bash
simple new trigger:webhook <app> <name> <display_name> [flags]
```

**Flags:**
| Flag | Required | Default | Description | Example |
|------|----------|---------|-------------|---------|
| `--action` | Yes | - | The action to link. | `--action handle-payment` |
| `--method` | No | `post` | HTTP method: `get`, `post`, `put`, `delete`. | `--method post` |
| `--public` | No | `false` | Make endpoint public. | `--public` |

**Example:**

_Public webhook for payment callbacks:_

```bash
simple new trigger:webhook com.company.crm payment-hook "Payment Hook" \
  --action handle-payment \
  --method post \
  --public
```

---

## Contributing

This section is for developers contributing to the **Simple CLI** codebase.

### Repository Structure

```
tools/simple-cli/
├── cmd/simple/         # Entry point (main.go)
├── internal/
│   ├── cli/            # Command implementations (Cobra)
│   ├── scaffold/       # Logic for file generation
│   ├── fsx/            # Filesystem interfaces (for testing)
│   └── scaffold/templates/ # Embedded templates
```

### Architecture Constraints

1.  **Dependency Injection**: Never use `os.Open` directly in logic. Use `fsx.FileSystem` interface.
2.  **Zero-IO Tests**: All unit tests must use `fsx.MockFileSystem`. We target 90%+ coverage.
3.  **Embedded Templates**: All scaffolding assets are compiled into the binary using `//go:embed`.

### Adding a Command

1.  Create `internal/cli/<command>.go`.
2.  Define the Cobra command struct.
3.  Implement logic using `fsx.FileSystem`.
4.  Add unit tests in `internal/cli/<command>_test.go` using mocks.

### Testing

```bash
# Run tests
go test -cover ./...

# Lint
golangci-lint run ./...
```

See [AGENTS.md](../../AGENTS.md) for full engineering standards.

---

## License

Apache 2.0
