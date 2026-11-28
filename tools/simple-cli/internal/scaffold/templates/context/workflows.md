# Simple Platform AI Playbook

This document defines the **standard operating procedure** for building and modifying Simple Platform applications.

> [!NOTE]
> **Enterprise Standard**: Simple Platform is an ENTERPRISE business platform.
>
> - **Think Big:** Plan for feature-rich, performant, scalable, and high-UX solutions.
> - **Feedback Loop:** User feedback is vital. Prompt for input on critical decisions.
> - **AI-Friendly:** Write code that is high cohesion, low coupling, and easy for AI to evolve.

## Core Workflow: The Lifecycle Decision Tree

**Is it a new app?**

### YES:

1.  **Create a new app**:
    - Command: `simple new app <app_id> "<Name>"`
    - _Ref:_ `.simple/context/cli-manifest.json` (Command: "new app")

2.  **Work with user to define data model**:
    - **List of Tables**: (singular and plural names, id prefix)
    - **List of Fields**: (name, type, constraints, display names, position)
    - **List of Field Relationships**: (display name, type, target table and fields)
      - Relationship with a table in existing app or external app?
      - **CRITICAL**: For every `belongs_to` relationship, create inverse `has_many` or `has_one` relationship.
      - Join-table information with `has_many` relationships.
    - **List of Indexes**: (name, fields, constraints)
    - _Ref:_ `.simple/context/scl-grammar.txt` (Section: Tables & Fields)
    - _Ref:_ `.simple/context/03-data-layer-scl.md` (Concepts: Schema & Relations)

3.  **Figure out Record Behaviors** (Once data model is approved):
    - For each table, each field -- `load`, `update`, and `submit` events.
    - _Ref:_ `.simple/context/08-record-behaviors.md` (Concepts: Events)

4.  **Figure out any Custom Actions** (Once record behaviors are approved):
    - _Condition:_ Only when record behaviors cannot handle the logic.
    - _Type:_ `db-event`, `time-based`, `webhook` (external request).
    - _Ref:_ `.simple/context/07-actions-and-triggers.md` (Concepts: Actions)

5.  **Proceed to Implementation** (Once Data Model, Behaviors, and Actions are approved):
    - **Tables** (table, fields, in-app relationships and indexes):
      - **Definition:** `apps/<app_id>/tables.scl`
      - _Ref:_ `.simple/context/scl-grammar.txt` (Section: Table Syntax)
    - **External App Relationships** (display names, field positions):
      - **Definition:** `apps/<app_id>/records/` folder files.
      - _Ref:_ `.simple/context/05-app-records-overview.md` (Concepts: Records)
      - _Ref:_ `.simple/context/06-metadata-configuration.md` (Concepts: Metadata)
    - **Record Behaviors**:
      - **Command:** `simple new behavior <app_id> <table_name>`
      - JS Scripts: `apps/<app_id>/scripts/record-behaviors/<table-name>.js`
      - Test: `apps/<app_id>/scripts/record-behaviors/<table-name>.test.js`
      - Registration: `apps/<app_id>/records/10_behaviors.scl`
      - **Verify:** `simple test <app_id> --behavior <table_name>`
      - _Ref:_ `.simple/context/cli-manifest.json` (Command: "new behavior")
    - **Custom Actions & Triggers**:
      - **Commands:**
        - `simple new action <app_id> <action_name> "<Display name>" --lang ts --scope <org>`
        - `simple new trigger:db <app_id> ...`
        - `simple new trigger:timed <app_id> ...`
        - `simple new trigger:webhook <app_id> ...`
      - Write proper code and tests.
      - **Verify:** `simple test <app_id> --action <action_name>`
      - **Registration** (in `apps/<app_id>/records/`):
        - Action in `logic` table.
        - Trigger in `trigger` table.
        - Binding in `logic_trigger` table.
        - _Ref:_ `.simple/context/scl-grammar.txt` (Section: Logic Binding)
      - _Ref:_ `.simple/context/cli-manifest.json` (Commands: "new action", "new trigger:\*")
    - **Seed Data**:
      - Add seed records in `apps/<app_id>/records/` files.
      - _Ref:_ `.simple/context/05-app-records-overview.md` (Concepts: Seed Data)

### NO (Existing App / Modification):

1.  **Figure out if it's a change in**:
    - Table, Field, Record Behavior, Action (logic), Trigger, Logic-Trigger, Seed Data.
2.  **Follow steps above based on change type**:
    - e.g., if changing a table, follow "Tables" implementation steps.
    - e.g., if adding logic, check "Record Behavior" vs "Custom Action" rules.

### Finalization:

1.  `simple lint` (or `simple lint <target>` for one app or Action)
2.  `simple build <app_id>` (when Actions or Spaces exist)
3.  `simple test`
4.  `simple deploy apps/<app_id> --env <env>` (when deployment is requested)
    - _Ref:_ `.simple/context/cli-manifest.json` (Command: "deploy")

Action and space dependencies use their `package-lock.json` files. `simple build`
and `simple test` run `npm ci --ignore-scripts` when a lock file is present; the
first install uses `npm install --ignore-scripts`. After changing a dependency
in `package.json`, run `simple deps update <app>` and commit the changed lock
files before deploying.

## Git Checks

The workspace installs six Git hooks through `core.hooksPath=.githooks`:

- `pre-commit` scans staged files for hidden code and install-time scripts, then runs ESLint on staged files. Restore a hidden-code file from a trusted version, remove an unapproved install script, or fix ESLint and stage the correction.
- `commit-msg` checks Conventional Commits. Keep the whole header at or below 72 characters, use a lower-case subject after the colon, and omit a final period. Example: `feat(com.acme.demo): add a greeting action`.
- `pre-push` scans the full tree and runs `simple lint`. Fix each named guard finding and each lint error before pushing.
- `post-checkout`, `post-merge`, and `post-rewrite` scan the full tree. Fix any named hidden-code or install-script finding before the next commit or push.

Never bypass a hook with `--no-verify`. If a hook cannot find `node`, `npx`, or `simple`, enter the development environment with `devbox shell` and run Git there.

## Create an Agent

When a developer asks for an agent, follow `.agent/workflows/create-agent.md` and use the `build-agents` skill. Create the agent and kind of work with the CLI, then complete the generated persona, playbook, and shape file. Add only the tools and longer procedures the work needs. See `.simple/context/13-agents.md` for the parts, command flags, and build checks.
