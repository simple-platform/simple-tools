<h1 align="center">Simple CLI</h1>

<p align="center">
  <strong>The command-line interface for the Simple Platform</strong><br>
  <em>Build, test, and deploy enterprise applications</em>
</p>

<p align="center">
  <a href="../../LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License"></a>
  <a href="#installation"><img src="https://img.shields.io/badge/go-%3E%3D1.25-00ADD8.svg" alt="Go Version"></a>
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

**Prerequisites**: Go 1.25.4 or later ([Download](https://golang.org/dl/))

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

Run tests for applications, actions, or record behaviors using Vitest.

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
| `--coverage` | | `false` | Enable code coverage reporting. |
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
| `--scope` | `-s` | **Required** | The NPM scope for the package (without `@`). |
| `--env` | `-e` | `server` | Execution environment: `server`, `client`, or `both`. |
| `--desc` | `-d` | `""` | Description of the action. |
| `--lang` | `-l` | `ts` | Programming language (currently only `ts` is supported). |

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
```

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
