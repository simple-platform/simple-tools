---
trigger: glob
globs: "apps/*/app.scl"
---
# App Versioning Rules

> [!IMPORTANT]
> Versions allow safe rollbacks and dependency management.

## 1. SemVer Compliance
*   **Format:** `X.Y.Z` or `X.Y.Z-ENV` (e.g., `1.0.0`, `2.1.0-dev`).
*   **Stable:** `MAJOR.MINOR.PATCH` (e.g., `1.0.0`) for production-ready code.
*   **Pre-Release:** `MAJOR.MINOR.PATCH-ENV` (e.g. `1.1.0-dev`, `1.1.0-staging`) for testing.
*   **Forbidden:** Dates (`2024.01`), Build numbers (`123`), or Prefixes (`v1.0`).

## 2. Deployment Logic
*   **Automated Bumping:** The `simple deploy` command can calculate the next version for you.
    *   `simple deploy app --bump minor` -> Bumps `1.2.0` to `1.3.0`.
*   **Explicit Versioning:** You can force a specific version.
    *   `simple deploy app --version 2.0.0-beta` -> Sets version to `2.0.0-beta`.
*   **Rule:** You **MUST** use either `--bump` or `--version` when deploying logic changes.

## 3. Immutable History
*   **Constraint:** You cannot change the definition of an *already deployed* version.
*   **Action:** If you change code/schema, you **MUST** bump the version on next deployment.
