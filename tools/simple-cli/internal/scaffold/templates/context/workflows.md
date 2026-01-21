# Simple Platform AI Playbook

This document defines the **standard operating procedure** for building and modifying Simple Platform applications.

> [!NOTE]
> **Enterprise Standard**: Simple Platform is an ENTERPRISE business platform.
> *   **Think Big:** Plan for feature-rich, performant, scalable, and high-UX solutions.
> *   **Feedback Loop:** User feedback is vital. Prompt for input on critical decisions.
> *   **AI-Friendly:** Write code that is high cohesion, low coupling, and easy for AI to evolve.

## Core Workflow: The Lifecycle Decision Tree

**Is it a new app?**

### YES:
1.  **Create a new app**:
    *   Command: `simple new app <app_id> "<Name>"`
    *   *Ref:* `.simple/context/cli-manifest.json` (Command: "new app")

2.  **Work with user to define data model**:
    *   **List of Tables**: (singular and plural names, id prefix)
    *   **List of Fields**: (name, type, constraints, display names, position)
    *   **List of Field Relationships**: (display name, type, target table and fields)
        *   Relationship with a table in existing app or external app?
        *   **CRITICAL**: For every `belongs_to` relationship, create inverse `has_many` or `has_one` relationship.
        *   Join-table information with `has_many` relationships.
    *   **List of Indexes**: (name, fields, constraints)
    *   *Ref:* `.simple/context/scl-grammar.txt` (Section: Tables & Fields)
    *   *Ref:* `.simple/context/03-data-layer-scl.md` (Concepts: Schema & Relations)

3.  **Figure out Record Behaviors** (Once data model is approved):
    *   For each table, each field -- `load`, `update`, and `submit` events.
    *   *Ref:* `.simple/context/08-record-behaviors.md` (Concepts: Events)

4.  **Figure out any Custom Actions** (Once record behaviors are approved):
    *   *Condition:* Only when record behaviors cannot handle the logic.
    *   *Type:* `db-event`, `time-based`, `webhook` (external request).
    *   *Ref:* `.simple/context/07-actions-and-triggers.md` (Concepts: Actions)

5.  **Proceed to Implementation** (Once Data Model, Behaviors, and Actions are approved):
    *   **Tables** (table, fields, in-app relationships and indexes):
        *   **Definition:** `apps/<app_id>/tables.scl`
        *   *Ref:* `.simple/context/scl-grammar.txt` (Section: Table Syntax)
    *   **External App Relationships** (display names, field positions):
        *   **Definition:** `apps/<app_id>/records/` folder files.
        *   *Ref:* `.simple/context/05-app-records-overview.md` (Concepts: Records)
        *   *Ref:* `.simple/context/06-metadata-configuration.md` (Concepts: Metadata)
    *   **Record Behaviors**:
        *   **Command:** `simple new behavior <app_id> <table_name>`
        *   JS Scripts: `apps/<app_id>/scripts/record-behaviors/<table-name>.js`
        *   Test: `apps/<app_id>/scripts/record-behaviors/<table-name>.test.js`
        *   Registration: `apps/<app_id>/records/10_behaviors.scl`
        *   **Verify:** `simple test <app_id> --behavior <table_name>`
        *   *Ref:* `.simple/context/cli-manifest.json` (Command: "new behavior")
    *   **Custom Actions & Triggers**:
        *   **Commands:**
            *   `simple new action <app_id> <action_name> --scope ...`
            *   `simple new trigger:db <app_id> ...`
            *   `simple new trigger:timed <app_id> ...`
            *   `simple new trigger:webhook <app_id> ...`
        *   Write proper code and tests.
        *   **Verify:** `simple test <app_id> --action <action_name>`
        *   **Registration** (in `apps/<app_id>/records/`):
            *   Action in `logic` table.
            *   Trigger in `trigger` table.
            *   Binding in `logic_trigger` table.
            *   *Ref:* `.simple/context/scl-grammar.txt` (Section: Logic Binding)
        *   *Ref:* `.simple/context/cli-manifest.json` (Commands: "new action", "new trigger:*")
    *   **Seed Data**:
        *   Add seed records in `apps/<app_id>/records/` files.
        *   *Ref:* `.simple/context/05-app-records-overview.md` (Concepts: Seed Data)

### NO (Existing App / Modification):
1.  **Figure out if it's a change in**:
    *   Table, Field, Record Behavior, Action (logic), Trigger, Logic-Trigger, Seed Data.
2.  **Follow steps above based on change type**:
    *   e.g., if changing a table, follow "Tables" implementation steps.
    *   e.g., if adding logic, check "Record Behavior" vs "Custom Action" rules.

### Finalization:
1.  `simple build`
2.  `simple test`
3.  `simple deploy <app_id> --env <env>`
    *   *Ref:* `.simple/context/cli-manifest.json` (Command: "deploy")
