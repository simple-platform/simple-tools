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

- **Usage:** `simple new app <app-id> <name>`
- **Args:**
  - `<app-id>`: Unique identifier (e.g., `com.acme.crm`).
  - `<name>`: Human-readable display name (e.g., "Customer CRM").
- **Flags:**
  - `--desc <string>`: Application description.

### `simple new action`

Create a new action, in TypeScript or in Rust, with boilerplate that builds and
tests as written.

- **Usage:** `simple new action <app-id> <name> <display-name>`
- **Args:**
  - `<app-id>`: Target App ID.
  - `<name>`: Action name in kebab-case (e.g., `calculate-tax`).
  - `<display-name>`: Human readable label.
- **Flags:**
  - `--lang <string>`: Action language (`ts`, `rust`). Defaults to `ts`.
  - `--scope <string>`: NPM package scope (e.g., `acme`). **REQUIRED for `--lang ts`.** A Rust action is a cargo crate named after the action itself and is never published to a registry, so it takes no scope.
  - `--env <string>`: Execution environment (`server`, `client`, `both`). Defaults to `server`.
  - `--desc <string>`: Description of the logic.

### `simple new agent`

Create an agent and its persona file and knowledge record.

- **Usage:** `simple new agent <app-id> <name> "<Display name>" [--desc <description>] [--json]`
- **Args:**
  - `<app-id>`: Existing target app.
  - `<name>`: Lowercase name using letters, digits, and hyphens; it starts with a letter.
  - `<Display name>`: Required, one line, no double quote, up to 100 characters.
- **Flags:**
  - `--desc <description>`: Optional description, one line, no double quote, up to 1,000 characters.
  - `--json`: Use the root JSON output format.

The command reports files that need their TODO lines completed. It refuses an existing agent and does not duplicate records on a retry.

### `simple new task-type`

Create a kind of work for an agent or for people. Agent-owned work gets `knowledge/playbook-<kind-of-work>.md`, input and output shapes, a default agent, and tool links.

- **Usage:** `simple new task-type <app-id> <name> "<Plural name>" [--agent <agent-name>] [--tools a,b] [--every <duration>] [--no-agent] [--desc <description>] [--json]`
- **Args:**
  - `<app-id>`: Existing target app.
  - `<name>`: Lowercase name using letters, digits, hyphens, or underscores; it starts with a letter.
  - `<Plural name>`: Required, one line, no double quote, up to 100 characters.
- **Flags:**
  - `--agent <agent-name>`: Default agent. May be omitted only when the app has exactly one agent.
  - `--tools a,b`: Comma-separated known platform tools or existing app actions.
  - `--every <duration>`: Optional interval such as `90s`, `30m`, or `1h`; whole seconds, at least one second.
  - `--no-agent`: Create work only people do. It creates the kind-of-work record, shape file, and states file. It creates no playbook, agent link, or carried links. Do not combine this flag with `--agent`, `--tools`, or `--every`.
  - `--desc <description>`: Optional description, one line, no double quote, up to 1,000 characters.
  - `--json`: Use the root JSON output format.

If the kind of work exists, use `simple new skill` or `simple new tool` to add to it. If a tool is not known, create it with `simple new tool`. Interval work needs a tool that reports changes since its last turn.

For a free-text answer, set the shape file's `output` to `null` or leave that field out. The build treats both the same way.

### `simple new skill`

Create a skill and link it to a kind of work, or add a missing link for an existing skill.

- **Usage:** `simple new skill <app-id> <name> ["<Title>"] --task-type <task-type-name> [--desc <description>] [--json]`
- **Args:**
  - `<app-id>`: Existing target app.
  - `<name>`: Lowercase name using letters, digits, and hyphens; it starts with a letter.
  - `<Title>`: Required for a new skill, one line, no double quote, up to 255 characters. Omit for an existing skill.
- **Flags:**
  - `--task-type <task-type-name>`: Required kind of work to receive the skill.
  - `--desc <description>`: Optional description; for a new skill this becomes its Purpose paragraph.
  - `--json`: Use the root JSON output format.

New skills need the eight sections in order: Purpose, When To Use, Required Context, Procedure, Good Examples, Bad Examples And Why, Error Recovery, Completion Checklist. Keep the generated `Slug:` line. Do not put scripts, queries, or code in a skill.

### `simple new knowledge`

Create reference knowledge and link it to a kind of work, or add a missing link for an existing body.

- **Usage:** `simple new knowledge <app-id> <name> ["<Title>"] --task-type <task-type-name> [--json]`
- **Args:**
  - `<app-id>`: Existing target app.
  - `<name>`: Lowercase name using letters, digits, and hyphens; it starts with a letter. It must not start with `skill-`, `tool-`, or `playbook-`.
  - `<Title>`: Required for a new body; omit it for an existing body. One line, no double quote, up to 255 characters.
- **Flags:**
  - `--task-type <task-type-name>`: Required kind of work to receive the knowledge.
  - `--json`: Use the root JSON output format.

The command creates `knowledge/<name>.md` with a title line and a `TODO(simple):` line, its knowledge record, and a link at the next position. Reference knowledge is plain written material for the agent to read, such as a list of terms or standing answers. It has no required sections. Use a skill for a way of working; skills have the eight sections above. An existing body gets only a missing link. If both already exist, the command succeeds and says there was nothing to add.

### `simple new tool`

Set up a tool and its lesson. Optionally link an app action to a kind of work.

- **Usage:** `simple new tool <app-id> <name> ["<Display name>"] [--task-type <task-type-name>] [--lang ts|rust] [--scope <org>] [--env <env>] [--desc <description>] [--json]`
- **Args:**
  - `<app-id>`: Existing target app.
  - `<name>`: Platform tool or app action name; an unknown name can be created as a new action.
  - `<Display name>`: Optional for a known platform tool or action; required for a new action. One line, no double quote, up to 100 characters.
- **Flags:**
  - `--task-type <task-type-name>`: Optional kind of work to receive the tool. A platform tool requires this flag.
  - `--lang <string>`: Language for a new action (`ts`, `rust`); defaults to `ts`.
  - `--scope <string>`: NPM package scope; required for a new TypeScript action and unused for Rust.
  - `--env <string>`: New action environment (`server`, `client`, `both`); defaults to `server`.
  - `--desc <description>`: Optional one-line description, no double quote, up to 1,000 characters.
  - `--json`: Use the root JSON output format.

Without `--task-type`, the command sets up the tool and its lesson without linking it to a kind of work. A platform tool without `--task-type` is refused because there is no app action to set up. An unknown name without a display name is refused with the command needed to create it. A tool lesson uses the eight sections listed for `simple new skill` and says what the tool takes, refuses, and returns. Do not put scripts, queries, or code in a lesson.

### `simple new behavior`

Create a client-side record behavior script.

- **Usage:** `simple new behavior <app-id> <table-name>`
- **Args:**
  - `<app-id>`: Target App ID.
  - `<table-name>`: The table this behavior attaches to.

### `simple new space`

Scaffold a new custom React Space.

- **Usage:** `simple new space <app-id> <name> <display-name>`
- **Args:**
  - `<app-id>`: Target App ID (e.g., `com.acme.crm`).
  - `<name>`: Space name in kebab-case (e.g., `sales-dashboard`).
  - `<display-name>`: Human readable label.
- **Flags:**
  - `--desc <string>`: Description of the space.

Build an app's Space bundles with `simple build <app-id>`. Run a Space's tests with
`simple test <app-id> --space <name>`. Use the configured DevStudio workflow for
integrated preview; check `simple devstudio serve --help` for the installed CLI.

### `simple new trigger:db`

Create a database event trigger.

- **Usage:** `simple new trigger:db <app-id> <name> <display-name>`
- **Args:**
  - `<app-id>`: Target App ID.
  - `<name>`: Trigger name (snake_case).
  - `<display-name>`: Human readable label.
- **Flags:**
  - `--table <string>`: **REQUIRED** Table to watch.
  - `--ops <string>`: Operations to watch: `insert`, `update`, `delete` (comma-separated, default: `insert`).
  - `--action <string>`: Action to execute.
  - `--condition <string>`: JQ condition filter.
  - `--desc <string>`: Description.

### `simple new trigger:timed`

Create a scheduled timed trigger.

- **Usage:** `simple new trigger:timed <app-id> <name> <display-name>`
- **Args:**
  - `<app-id>`: Target App ID.
  - `<name>`: Trigger name (snake_case).
  - `<display-name>`: Human readable label.
- **Flags:**
  - `--frequency <string>`: `minutely`|`hourly`|`daily`|`weekly`|`monthly`|`yearly`.
  - `--interval <int>`: Interval between runs (default: 1).
  - `--start-at <string>`: Start time (ISO8601).
  - `--time <string>`: Time of day (HH:MM:SS) (default: "00:00:00").
  - `--timezone <string>`: Timezone (default: "UTC").
  - `--days <string>`: Specific days (MON,TUE...).
  - `--action <string>`: Action to execute.
  - `--desc <string>`: Description.

### `simple new trigger:webhook`

Create an HTTP webhook trigger.

- **Usage:** `simple new trigger:webhook <app-id> <name> <display-name>`
- **Args:**
  - `<app-id>`: Target App ID.
  - `<name>`: Trigger name (snake_case).
  - `<display-name>`: Human readable label.
- **Flags:**
  - `--method <string>`: HTTP method: `get`|`post`|`put`|`delete` (default: `post`).
  - `--public`: Make endpoint public (no auth).
  - `--action <string>`: Action to execute.
  - `--desc <string>`: Description.

## 3. Development Commands

### `simple build`

Compile all SCL files, Actions (WASM), and Spaces (Vite/React).

- **Usage:** `simple build [target]`
- **Args:**
  - `[target]` (Optional): Specific app (`com.acme.crm`) or action (`com.acme.crm/my-action`) to build.
- **Flags:**
  - `--all`: Build all actions in all apps.
  - `--concurrency <int>`: Number of parallel builds (default: 4).
- **Description:** Validates schema integrity, transpiles TypeScript to WASM, bundles spaces, and checks agent guidance for app or all-app builds. A single action or space build does not run agent checks. An app with agent records and no actions or spaces is still a valid build target.

The marker scan reports each `TODO(simple):` line in `knowledge/**/*.md`, `records/**/*.scl`, `records/**/*.json`, and app action sources under `actions/*/src/**`. Wiring and structure checks run when an app has an agent or kind-of-work record. A kind of work with no linked agent is left out of the default-agent, playbook, and carried-link checks. Other kinds of work need exactly one default agent, a playbook, and links to both the agent's persona and playbook. Every shape needs an object `input`; `output` may be an object, `null`, or absent. Referenced state files must name all seven standard states. Tool links must point to known platform tools or app actions marked as tools, and every app action marked as a tool needs a lesson.

The build checks a carried body as a skill only when its file name starts with `skill-` or it contains a `Slug:` line. The file name and slug must agree; a `skill-` file without a `Slug:` line is a problem. Other carried bodies are reference knowledge and only need non-empty, valid text. Every tool lesson gets the full skill check. Problems show the file, line, and rule to fix. `simple deploy` runs the same checks before it collects files.

### `simple install`

Install a deployed app to an environment (migrations, cache warming).

- **Usage:** `simple install <app-id>`
- **Args:**
  - `<app-id>`: App ID to install (must be already deployed).
- **Flags:**
  - `--env <string>`: **REQUIRED**. Target environment (`dev`, `staging`, `prod`).
  - `--progress <string>`: `auto` (default), `tty` or `plain`. Use `plain` for one line per step when capturing output.
  - `--json`: Print one JSON document on stdout; errors go to stderr as `{"error": ...}`.

### `simple cleanup`

Permanently delete tables and fields, and their data, from an app in an environment. An install refuses a version that would drop either; this is the separate step that removes them on purpose. It shows everything that will be deleted before it asks. Run it without `--yes` first and show the user the plan.

- **Usage:** `simple cleanup <app-id> --env <env> [--table <table>]... [--field <table.field>]...`
- **Args:**
  - `<app-id>`: App ID to clean up.
- **Flags:**
  - `--env <string>`: **REQUIRED**. Target environment (`dev`, `staging`, `prod`).
  - `--table <string>`: Table to remove. Repeatable.
  - `--field <string>`: Field to remove, written `table.field` (exactly one dot). Repeatable. At least one `--table` or `--field` is required.
  - `--yes`: Remove without asking for confirmation. Without it, a terminal asks for the app id to be typed; without a terminal the command only prints the plan and removes nothing. On a production environment the server accepts a removal only when the app id was typed, so `--yes` is refused there: ask the user to run the command in a terminal and type the app id.
  - `--json`: Never asks. Print one JSON document on stdout, `{"plan": ..., "executed": false}`, or with `--yes` `{"plan": ..., "executed": true}` once removed; errors go to stderr as `{"error": ...}`.
- **Output:** One line per table or field with its row count (for a field, the rows that hold a value), the metadata that goes with it, an `also removes from <app-id>: <count> <what>` line for what the removal also takes from other applications (for example `also removes from com.acme.billing: 2 db events`), and a `blocked:` line for each thing that stops it. A blocked plan removes nothing and exits 1. After a removal, install the version again with `simple install <app-id> --env <env>`.

### `simple test`

Run the unified test runner. Each target is handed to its own runner: Vitest for
TypeScript and JavaScript, `cargo test` for Rust actions.

- **Usage:** `simple test [app-id]`
- **Args:**
  - `[app-id]` (Optional): Limit tests to a specific app.
- **Flags:**
  - `--action <string>`: Run tests for a specific action only.
  - `--behavior <string>`: Run tests for a specific behavior script only.
  - `--space <string>`: Run tests for a specific space only.
  - `--coverage`: Enable code coverage reporting.
  - `--json`: Output results in JSON format (CI/CD friendly).

## 4. Operational Commands

### `simple deploy`

Deploy application artifacts to a remote environment.

- **Usage:** `simple deploy <app-path>`
- **Args:**
  - `<app-path>`: Path to the app directory (e.g., `apps/com.acme.crm`).
- **Flags:**
  - `--env <string>`: **REQUIRED**. Target environment (`dev`, `staging`, `prod`).
  - `--bump <string>`: Semver bump strategy (`patch`, `minor`, `major`).
  - `--no-install`: Publish the version without installing it; install it later with `simple install <app-id> --env <env>`.
  - `--dry-run`: Show the next version and the files that would be uploaded. Nothing is written or uploaded: no sign-in, no connection, `app.scl` unchanged.
  - `--progress <string>`: `auto` (default), `tty` or `plain`. Use `plain` for one line per step when capturing output.
  - `--json`: Print one JSON document on stdout; errors go to stderr as `{"error": ...}`. An install that fails after the publish still exits 1.
- **Output:** One line per step (`[n/N] ✓ Step: detail (duration)`) when not on a terminal. Success ends with `✅ Deployed <app-id>@<version> (Installed) in <duration>`. After a failure or Ctrl+C, the lines before the error say what was left behind (a bumped `app.scl`, a publish or install the server may still finish) and what to run next.

### `simple init`

Initialize a new workspace (Monorepo).

- **Usage:** `simple init <project-name>`
- **Args:**
  - `<project-name>`: Name of the root directory to create.
- **Flags:**
  - `--tenant <string>`: Tenant name for `simple.scl` configuration.
