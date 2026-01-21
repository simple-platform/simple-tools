---
description: Decision tree and execution guide for implementing business logic.
---
# Workflow: Add Business Logic

> [!NOTE]
> **Ref:** `.simple/context/workflows.md` (Lifecycle Decision Tree)

## Phase 1: The Decision Tree
**Ask:** "What triggers this logic?"

1.  **UI/Form Interaction?** (Validation, visibility, computed fields)
    *   **Selected:** Record Behavior.
    *   **Goto:** Phase 2.

2.  **Database Change?** (Async side-effect after insert/update)
    *   **Selected:** Custom Action (`db-event`).
    *   **Goto:** Phase 3.

3.  **Time Schedule?** (Cron job, nightly report)
    *   **Selected:** Custom Action (`time-based` Trigger).
    *   **Goto:** Phase 3.

4.  **External Request?** (Webhook, API Integration)
    *   **Selected:** Custom Action (`webhook` Trigger).
    *   **Goto:** Phase 3.

## Phase 2: Record Behavior Implementation
1.  **Command:** `simple new behavior <app_id> <table_name>`
2.  **File:** `apps/<app_id>/scripts/record-behaviors/<table_name>.js`
3.  **Events:** Implement logic for `load`, `update`, `submit`.
4.  **Registration:** Check `records/10_behaviors.scl`.
5.  **Test:** `simple test <app_id> --behavior <table_name>`

## Phase 3: Custom Action Implementation
1.  **Command:** `simple new action <app_id> <action_name> --scope ...`
2.  **File:** `apps/<app_id>/actions/<action_name>/index.ts`
3.  **Registration (The "Binding" Pattern):**
    *   **Logic:** Define `logic` record in `records/20_logic.scl`.
    *   **Trigger:** Define `trigger` record in `records/20_triggers.scl`.
        *   Type: `db_event` OR `time_based` OR `webhook`.
    *   **Binding:** Define `logic_trigger` in `records/30_links.scl`.
4.  **Test:** `simple test <app_id> --action <action_name>`
