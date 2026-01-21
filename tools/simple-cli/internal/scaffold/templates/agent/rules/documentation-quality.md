---
trigger: glob
globs:
  - "README.md"
  - "**/*.scl"
  - "**/*.ts"
  - "**/*.js"
---
# Documentation Quality Rules

> [!IMPORTANT]
> Code is read 10x more than it is written. Optimize for the reader.

## 1. README Requirements
*   **Mandatory:** Root `README.md` must exist.
*   **Content:** Must explain *How to Run*, *Architecture*, and *Dependencies*.

## 2. Self-Documenting Code
*   **Naming:** Variable names should explain their purpose (`daysUntilExpiry` vs `d`).
*   **Comments:** Use comments to explain *complex algorithm choices* or *business logic anomalies*.
*   **SCL:** Every critical field (money, status enums, foreign keys) should have a comment explaining its source or constraint.

## 3. Evolution Ready
*   **Why:** When documenting, assume the next reader (or AI) knows nothing about the current context.
*   **Context:** Link to relevant Tickets or Specs if a decision is non-obvious.
