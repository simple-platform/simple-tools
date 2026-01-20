---
name: simple-cli
description: definitive reference for the `simple` CLI. Contains ALL commands, arguments, and flags.
---
# Simple CLI Skill

## 1. Global Usage
`simple <command> <subcommand> [args] [flags]`

## 2. Scaffolding Commands

### `simple new app`
Scaffold a new empty application structure.
*   **Usage:** `simple new app <app-id> <name>`
*   **Args:**
    *   `<app-id>`: Unique identifier (e.g., `com.acme.crm`).
    *   `<name>`: Human-readable display name (e.g., "Customer CRM").
*   **Flags:**
    *   `--desc <string>`: Application description.

### `simple new action`
Create a new server-side action with TypeScript boilerplate.
*   **Usage:** `simple new action <app-id> <name> <display-name>`
*   **Args:**
    *   `<app-id>`: Target App ID.
    *   `<name>`: Action name in kebab-case (e.g., `calculate-tax`).
    *   `<display-name>`: Human readable label.
*   **Flags:**
    *   `--scope <string>`: **REQUIRED**. NPM package scope (e.g., `@acme`).
    *   `--env <string>`: Execution environment (`server`, `client`, `both`). Defaults to `server`.
    *   `--desc <string>`: Description of the logic.

### `simple new behavior`
Create a client-side record behavior script.
*   **Usage:** `simple new behavior <app-id> <table-name>`
*   **Args:**
    *   `<app-id>`: Target App ID.
    *   `<table-name>`: The table this behavior attaches to.

## 3. Development Commands

### `simple build`
Compile all SCL files and Actions (WASM).
*   **Usage:** `simple build`
*   **Description:** Validates schema integrity and transpiles TypeScript to WASM.

### `simple test`
Run the unified test runner (Vitest + SCL Linter).
*   **Usage:** `simple test [app-id]`
*   **Args:**
    *   `[app-id]` (Optional): Limit tests to a specific app.
*   **Flags:**
    *   `--action <string>`: Run tests for a specific action only.
    *   `--behavior <string>`: Run tests for a specific behavior script only.
    *   `--coverage`: Enable code coverage reporting.
    *   `--json`: Output results in JSON format (CI/CD friendly).

## 4. Operational Commands

### `simple deploy`
Deploy application artifacts to a remote environment.
*   **Usage:** `simple deploy <app-path>`
*   **Args:**
    *   `<app-path>`: Path to the app directory (e.g., `apps/com.acme.crm`).
*   **Flags:**
    *   `--env <string>`: **REQUIRED**. Target environment (`dev`, `staging`, `prod`).
    *   `--bump <string>`: Semver bump strategy (`patch`, `minor`, `major`).
    *   `--no-install`: Skip `npm install` before building.

### `simple init`
Initialize a new workspace (Monorepo).
*   **Usage:** `simple init <project-name>`
*   **Args:**
    *   `<project-name>`: Name of the root directory to create.
*   **Flags:**
    *   `--tenant <string>`: Tenant name for `simple.scl` configuration.
