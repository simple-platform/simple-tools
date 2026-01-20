trigger: glob
globs: "apps/*/records/*.scl"
---
# Record Lifecycle & Idempotency Rules

## 1. Logical Keys (The "Invariant Identity")
*   **Definition:** The second argument in `set type, logical_key { ... }`.
*   **CRITICAL RULE:** Logical keys MUST be globally unique per table and **stable across environments**.
    *   **Bad:** `user_1` (Implies a database ID, which changes).
    *   **Good:** `user_john_doe`, `setting_api_key`, `role_admin_access`.
*   **Purpose:** The platform uses this key to identify that "Configuration A" in Dev is the same as "Configuration B" in Prod, enabling safe updates/migrations.

## 2. Idempotency (Upsert Logic)
*   **Mechanism:** The `set` command acts as an **UPSERT** (Update or Insert) based on the Logical Key.
*   **Lifecycle:**
    1.  **Check:** Does a record with this `logical_key` exist in this environment?
    2.  **No:** CREATE a new record.
    3.  **Yes:** UPDATE the existing record with the fields defined in the block.
*   **Implication:** You can re-run SCL files safely. It will not create duplicates.

## 3. Directory Structure & Order
Files in `records/` are processed in lexical order. You MUST adhere to the numbering convention to ensure dependencies are resolved.

| File Pattern | Purpose | Examples |
| :--- | :--- | :--- |
| `10_*.scl` | **Base Data** | Seed data, Initial lists. |
| `20_*.scl` | **Logic Definitions** | `logic`, `trigger`, `webhook`, `db_event`. |
| `30_*.scl` | **Bindings** | `logic_trigger` (Depends on Logic + Trigger). |
| `100_*.scl` | **Metadata** | `table`, `table_field` config (Display names). |

## 4. Variable Dependencies
*   **Constraint:** You cannot reference a record in the same file execution cycle unless you use a variable lookup.
*   **Pattern:** Use `var` blocks with GraphQL to fetch IDs of records created in previous steps/files.
