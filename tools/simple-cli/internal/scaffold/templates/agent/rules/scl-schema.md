trigger: glob
globs: "**/tables.scl"
---
# SCL Schema Modeling Rules

## 1. Valid Field Types
You MUST use the exact atoms below. **DO NOT** use `uuid`, `text`, or SQL types.

| Atom | Description | Key Constraints |
| :--- | :--- | :--- |
| `:string` | Varchar | `length`, `regex`, `hash`, `secret` |
| `:char` | Fixed char | `length` |
| `:integer` | 4-byte int | `min`, `max` |
| `:smallint` | 2-byte int | `min`, `max` |
| `:bigint` | 8-byte int | `min`, `max` |
| `:decimal` | Precision | `digits`, `decimals` |
| `:float` | IEEE 754 | `min`, `max` |
| `:boolean` | True/False | `default` |
| `:date` | Date only | `default` |
| `:time` | Time only | `default` |
| `:datetime` | TimestampTZ | `default` |
| `:json` / `:jsonb` | JSON data | - |
| `:document` | Files | `allowed_types`, `max_size` |
| `:version` | Semver | - |
| `:enum` | Fixed list | `values` (NO SPACES) |

## 2. Invalid Types (Anti-Patterns)
*   **NO** `:uuid`: Use `:string` + `id_prefix` in table definition.
*   **NO** `:text`: Use `:string` + `multiline true`.

## 3. Mandatory Table Directives
```scl
table my_table {
  default :id, :timestamps, :userstamps
  id_prefix "ABC"
  display_field name
}
```

## 4. Relationship Enforcement
*   **Bidirectional:** Every `belongs :to` implies a `has :many` exists somewhere.
*   **Cross-App:** `belongs :to` uses fully qualified names (`app.table`).
*   **Cross-App Inverse:** `has :many` for external tables is defined dynamically via `table_relationship` records.
