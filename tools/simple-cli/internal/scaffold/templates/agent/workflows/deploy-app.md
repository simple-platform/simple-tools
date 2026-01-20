---
description: Production readiness checklist, testing, and deployment procedure.
---
# Workflow: Deploy Application

> [!NOTE]
> **Enterprise Standard**: Deployment is not just copying files. It is ensuring stability and security.

## Phase 1: Pre-Flight Audit ("Day 2" Operations)
**Ref:** `AGENTS.md` (Day 2 Best Practices)

1.  **Governance Check:**
    *   Verify `tables.scl`: Are we using `:string` + `id_prefix`? (No `uuid`).
    *   Verify `records/`: Are Logical Keys stable? (e.g. `role_admin`, not `role_1`).
    *   **Security:** Are all PII fields marked `secret: true`?
2.  **Schema Evolution:**
    *   **Rule:** You cannot DROP a column in Production without a deprecation cycle of at least one full release cycle (recommended minimum 30 days).
    *   **Check:** Does this deploy remove any fields? If so, STOP. Rename them to `_deprecated_` first.
3.  **Data Integrity:**
    *   Are all new `belongs :to` relationships required? If so, is there existing data that will fail validation?

## Phase 2: Verification
1.  **Test Suite:**
    *   Run: `simple test <app> --coverage`
    *   **Success Metrics:**
        *   All tests PASS.
        *   Coverage > 80% (Recommended).
2.  **Build Artifacts:**
    *   Run: `simple build <app>`
    *   **Check:** Ensure no WASM compilation errors.

## Phase 3: Deployment
1.  **Select Environment:**
    *   "Deploying to `dev`, `staging`, or `prod`?"
2.  **Execute:**
    *   Command: `simple deploy <app> --env <target>`
    *   *Flags:* `--bump minor` (if feature), `--bump patch` (if fix).
3.  **Monitor:**
    *   Watch the output stream.
    *   Confirm "Migration Applied".
    *   Confirm "Logic Updated".

## Phase 4: Final Handover
Provide the User with:
1.  **Instance URL:** `https://<tenant>-<env>.on.simple.dev`
2.  **GraphQL Endpoint:** `https://<tenant>-<env>.on.simple.dev/v1/graphql`
3.  **Changelog:** Summary of what changed (Tables, Logics, Behaviors).
