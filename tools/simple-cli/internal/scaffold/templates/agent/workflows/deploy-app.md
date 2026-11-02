---
description: Production readiness checklist, testing, and deployment procedure.
---

# Workflow: Deploy Application

> [!NOTE]
> **Enterprise Standard**: Deployment is not just copying files. It is ensuring stability and security.

## Phase 1: Pre-Flight Audit ("Day 2" Operations)

**Ref:** `AGENTS.md` (Day 2 Best Practices)

1.  **Governance Check:**
    - Verify `tables.scl`: Are we using `:string` + `id_prefix`? (No `uuid`).
    - Verify `records/`: Are Logical Keys stable? (e.g. `role_admin`, not `role_1`).
    - **Security:** Are all PII fields marked `secret: true`?
2.  **Schema Evolution:**
    - **Rule:** You cannot DROP a column in Production without a deprecation cycle of at least one full release cycle (recommended minimum 30 days).
    - **Check:** Does this deploy remove any fields? If so, inspect the supported cleanup/install plan and obtain authorization for data deletion. Do not rename a live field to simulate deprecation; that changes its identity.
3.  **Data Integrity:**
    - Are all new `belongs :to` relationships required? If so, is there existing data that will fail validation?

## Phase 2: Verification

1.  **Test Suite:**
    - Run: `simple test <app> --coverage`
    - **Success Metrics:**
      - All tests PASS.
      - Coverage > 80% (Recommended).
2.  **Build Artifacts:**
    - Run: `simple build <app>`
    - **Check:** Ensure no WASM compilation errors.

## Phase 3: Deployment

1.  **Select Environment:**
    - "Deploying to `dev`, `staging`, or `prod`?"
2.  **Execute:**
    - Command: `simple deploy apps/<app> --env <target>`
    - _Flags:_ `--bump minor` (if feature), `--bump patch` (if fix).
    - Preview first when useful: `simple deploy apps/<app> --env <target> --dry-run` reports the next version and upload files without signing in, contacting the server, uploading, or changing `app.scl`.
    - Use `--progress=plain` when capturing output to get one line per deploy step.
3.  **Monitor:**
    - Watch the output stream.
    - Check the reported publish/install steps and final state; do not rely on invented success strings.
    - On failure, inspect version changes and any completed publish before retrying.

## Phase 4: Final Handover

Provide the User with:

1.  **Instance URL:** Use the configured instance URL.
2.  **API Endpoint:** Report the configured/discovered endpoint; do not invent tenant URL patterns.
3.  **Changelog:** Summary of what changed (Tables, Logics, Behaviors).
