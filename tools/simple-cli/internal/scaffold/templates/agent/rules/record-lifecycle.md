trigger: glob
globs: "apps/*/records/*.scl"
---
# Record Lifecycle & Idempotency Rules

## 1. Logical Keys
*   **Definition:** The second argument in `set type, logical_key { ... }`.
*   **Installer identity:** `(application_id, target_table_id, logical_key)` in the current tenant/environment's `app_record` provenance. Keep the logical key stable for the same app-owned declaration.
*   **Database identity:** A logical key is not a database ID. Without an explicit ID, the installer generates a SimpleID locally; generated IDs need not match across environments. Existing provenance identifies the installed target record.
*   **Naming:** Use descriptive keys such as `user_john_doe` or `role_admin_access`. For declarations referenced by `$app_record("logical_key")`, keep keys unique across the app: that helper looks up the app and logical key without a target table.

## 2. Installation and Updates
*   **Mechanism:** `set` uses installer provenance and previously applied data; it is not an unconditional UPSERT.
*   **Lifecycle:**
    1.  When neither provenance nor an explicit-ID target exists, create the record and its provenance.
    2.  When the installed target still matches the last applied data, apply changed declarations; unchanged declarations require no write.
    3.  When the target has drifted, system-principal edits permit an update; user edits cause a skip. An existing explicit-ID target also goes through this ownership/drift decision.
*   **Implication:** Reinstalling the same declaration can be idempotent, but does not guarantee overwriting user changes. Renaming a key changes its installer identity; it is not an ordinary field update.

## 3. Directory Structure & Order
Files in `records/` are sorted by the numeric prefix before `_`: `major` or `major.minor`. For example, `9_*.scl` precedes `10_*.scl`, and `24.3_*.scl` precedes `24.4_*.scl`. Use distinct prefixes when dependencies require an order; do not rely on lexical sorting or ties. Blocks within a file run in declaration order.

| File Pattern | Purpose | Examples |
| :--- | :--- | :--- |
| `10_*.scl` | **Base Data** | Seed data, Initial lists. |
| `20_*.scl` | **Logic Definitions** | `logic`, `trigger`, `webhook`, `db_event`. |
| `30_*.scl` | **Bindings** | `logic_trigger` (Depends on Logic + Trigger). |
| `100_*.scl` | **Metadata** | `table`, `table_field` config (Display names). |

## 4. Variable Dependencies
*   **App-owned records:** Use `$app_record("logical_key")` for a record already installed by an earlier block or file, including an earlier block in the same file.
*   **Other records:** Use `var` blocks with GraphQL to resolve metadata or existing business records by stable attributes. Variables are evaluated before that file's records, so these queries cannot see records first created later in the same file. Do not guess environment-specific database IDs.
